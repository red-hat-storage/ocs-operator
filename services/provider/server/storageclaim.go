package server

import (
	"context"
	"fmt"
	"sync"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ocsStorageClassClaimManager struct {
	client    client.Client
	namespace string
	nameByUID map[types.UID]string
	mutex     sync.RWMutex
}

func newStorageClassClaimManager(ctx context.Context, cl client.Client, namespace string) (*ocsStorageClassClaimManager, error) {
	claims := &ocsv1alpha1.StorageClassClaimList{}
	err := cl.List(ctx, claims, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storageclassclaim. %v", err)
	}

	nameByUID := map[types.UID]string{}

	for _, claim := range claims.Items {
		nameByUID[claim.UID] = claim.Name
	}

	return &ocsStorageClassClaimManager{
		client:    cl,
		namespace: namespace,
		nameByUID: nameByUID,
	}, nil
}

// Create creates a new storageClassClaim resource, updates the claim cache and returns the storageClassClaim ID.
func (c *ocsStorageClassClaimManager) Create(ctx context.Context, name, consumerUUID, claimType, encryptionMethod string) (string, error) {
	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"storageConsumerUUID": consumerUUID,
			},
		},
		Spec: ocsv1alpha1.StorageClassClaimSpec{
			Type:             claimType,
			EncryptionMethod: encryptionMethod,
		},
	}

	err := c.client.Create(ctx, storageClassClaimObj)
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			klog.Warningf("storageClassClaim %q already exists", name)
			return "", err
		}
		return "", fmt.Errorf("failed to create storageClassClaim resource %q. %w", storageClassClaimObj.Name, err)
	}

	c.mutex.Lock()
	c.nameByUID[storageClassClaimObj.UID] = storageClassClaimObj.Name
	c.mutex.Unlock()

	klog.Infof("successfully created storageClassClaim resource %q", name)

	return string(storageClassClaimObj.GetUID()), nil
}

// Delete deletes the storageClassClaim resource using UID and updates the claim cache
func (c *ocsStorageClassClaimManager) Delete(ctx context.Context, id string) error {
	uid := types.UID(id)
	c.mutex.RLock()
	claimName, ok := c.nameByUID[uid]
	if !ok {
		klog.Warningf("no storageClassClaim found with ID %q", id)
		c.mutex.RUnlock()
		return nil
	}
	c.mutex.RUnlock()

	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      claimName,
			Namespace: c.namespace,
		},
	}
	if err := c.client.Delete(ctx, storageClassClaimObj); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			klog.Warningf("storageClassClaim %q not found.", claimName)
			return nil
		}
		return fmt.Errorf("failed to delete storageClassClaim %q. %v", claimName, err)
	}

	c.mutex.Lock()
	delete(c.nameByUID, uid)
	c.mutex.Unlock()

	klog.Infof("successfully deleted storageClassClaim resource %q", claimName)

	return nil
}
