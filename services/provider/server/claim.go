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

type ocsStorageClassClaim struct {
	client    client.Client
	nameByUID map[types.UID]string
	mutex     sync.RWMutex
}

// Create creates a new storageClassClaim resource, updates the consumer cache and returns the storageClassClaim UID
func (c *ocsStorageClassClaim) Create(ctx context.Context, name, claimType, encryptionMethod, lableStorageConsumerID string) error {
	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
			Labels: map[string]string{
				"storageConsumer": lableStorageConsumerID,
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
			return err
		}
		return fmt.Errorf("failed to create storageClassClaim resource %q. %v", storageClassClaimObj.Name, err)
	}

	c.mutex.Lock()
	c.nameByUID[storageClassClaimObj.UID] = storageClassClaimObj.Name
	c.mutex.Unlock()

	klog.Infof("successfully created storageClassClaim resource %q", name)

	return nil
}

// Get returns a storageClassClaim resource using the UID
func (c *ocsStorageClassClaim) Get(ctx context.Context, id string) (*ocsv1alpha1.StorageClassClaim, error) {
	uid := types.UID(id)

	c.mutex.RLock()
	consumerName, ok := c.nameByUID[uid]
	if !ok {
		c.mutex.RUnlock()
		klog.Errorf("no storageClassClaim found with the UID %q", id)
		return nil, fmt.Errorf("no storageClassClaim found with the UID %q", id)
	}
	c.mutex.RUnlock()

	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: consumerName}, storageClassClaimObj); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			return nil, fmt.Errorf("storageClassClaim resource %q not found. %v", consumerName, err)
		}
		return nil, fmt.Errorf("failed to get storageClassClaim resource with name %q. %v", consumerName, err)
	}

	return storageClassClaimObj, nil
}
