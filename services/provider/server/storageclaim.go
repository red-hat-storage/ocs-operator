package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type storageClassClaimManager struct {
	client    client.Client
	namespace string
}

func newStorageClassClaimManager(ctx context.Context, cl client.Client, namespace string) (*storageClassClaimManager, error) {
	return &storageClassClaimManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

// getStorageClassClaimName generates a name for a storageClassClaim resource.
func getStorageClassClaimName(consumerUUID, storageClassClaimName string) string {
	var s struct {
		StorageConsumerUUID   string `json:"storageConsumerUUID"`
		StorageClassClaimName string `json:"storageClassClaimName"`
	}
	s.StorageConsumerUUID = consumerUUID
	s.StorageClassClaimName = storageClassClaimName

	claimName, err := json.Marshal(s)
	if err != nil {
		klog.Errorf("failed to marshal a name for a storage class claim based on %v. %v", s, err)
		panic("failed to marshal storage class claim name")
	}
	name := md5.Sum([]byte(claimName))
	// The name of the StorageClassClaim is the MD5 hash of the JSON
	// representation of the StorageClassClaim name and storageConsumer UUID.
	return fmt.Sprintf("storageclassclaim-%s", hex.EncodeToString(name[:16]))
}

// Create creates a new storageClassClaim resource and returns the storageClassClaim ID.
func (s *storageClassClaimManager) Create(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer, storageClassClaimName, claimType, encryptionMethod string) error {
	consumerUUID := string(consumer.GetUID())
	generatedClaimName := getStorageClassClaimName(consumerUUID, storageClassClaimName)

	ownerRef := metav1.OwnerReference{
		UID:        consumer.UID,
		APIVersion: consumer.APIVersion,
		Kind:       consumer.Kind,
		Name:       consumer.Name,
	}
	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:            generatedClaimName,
			Namespace:       s.namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
			Labels: map[string]string{
				controllers.ConsumerUUIDLabel: consumerUUID,
				storageClassClaimNameLabel:    storageClassClaimName,
			},
		},
		Spec: ocsv1alpha1.StorageClassClaimSpec{
			Type:             claimType,
			EncryptionMethod: encryptionMethod,
		},
	}

	err := s.client.Create(ctx, storageClassClaimObj)
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create a StorageClassClaim named %q for consumer %q and claim %q. %w", generatedClaimName, consumerUUID, storageClassClaimName, err)
		}
		newStorageClassClaimObj := &ocsv1alpha1.StorageClassClaim{}
		getErr := s.client.Get(ctx, client.ObjectKey{Name: generatedClaimName, Namespace: s.namespace}, newStorageClassClaimObj)
		if getErr != nil {
			klog.Errorf("failed to get a StorageClassClaim named %q for consumer %q and claim %q. %v", generatedClaimName, consumerUUID, storageClassClaimName, getErr)
			return err
		}
		// check if the storageClassClaim is getting deleted.
		if newStorageClassClaimObj.DeletionTimestamp != nil {
			klog.Warningf("StorageClassClaim named %q for consumer %q and claim %q is already created but is getting deleted", generatedClaimName, consumerUUID, storageClassClaimName)
			return err
		}
		// check if the input is different
		if !reflect.DeepEqual(storageClassClaimObj.Spec, newStorageClassClaimObj.Spec) {
			klog.Errorf("StorageClassClaim named %q for consumer %q and claim %q is already exists with different spec (%v) but requested spec (%v)", generatedClaimName, consumerUUID, storageClassClaimName, storageClassClaimObj.Spec, newStorageClassClaimObj.Spec)
			return err
		}
	}

	klog.Infof("successfully created a StorageClassClaim resource %q for consumer %q and claim %q", generatedClaimName, consumerUUID, storageClassClaimName)

	return nil
}

// Delete deletes the storageClassClaim resource using storageClassClaimName
// and consumerUUID.
func (s *storageClassClaimManager) Delete(ctx context.Context, consumerUUID, storageClassClaimName string) error {
	generatedClaimName := getStorageClassClaimName(consumerUUID, storageClassClaimName)
	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedClaimName,
			Namespace: s.namespace,
		},
	}
	if err := s.client.Delete(ctx, storageClassClaimObj); err != nil {
		if kerrors.IsNotFound(err) {
			klog.Warningf("StorageClassClaim %q not found for consumer %q and claim %q", generatedClaimName, consumerUUID, storageClassClaimName)
			return nil
		}
		return fmt.Errorf("failed to delete StorageClassClaim %q for consumer %q and claim %q. %v", generatedClaimName, consumerUUID, storageClassClaimName, err)
	}

	klog.Infof("successfully deleted StorageClassClaim %q for consumer %q and claim %q", generatedClaimName, consumerUUID, storageClassClaimName)

	return nil
}

// Get returns the storageClassClaim resource using storageClassClaimName
// and consumerUUID.
func (s *storageClassClaimManager) Get(ctx context.Context, consumerUUID, storageClassClaimName string) (*ocsv1alpha1.StorageClassClaim, error) {
	generatedClaimName := getStorageClassClaimName(consumerUUID, storageClassClaimName)
	storageClassClaimObj := &ocsv1alpha1.StorageClassClaim{}
	err := s.client.Get(ctx, types.NamespacedName{Name: generatedClaimName, Namespace: s.namespace}, storageClassClaimObj)
	if err != nil {
		klog.Errorf("failed to get a StorageClassClaim named %q for consumer %q and claim %q. %v", generatedClaimName, consumerUUID, storageClassClaimName, err)
		return nil, err
	}

	return storageClassClaimObj, nil
}
