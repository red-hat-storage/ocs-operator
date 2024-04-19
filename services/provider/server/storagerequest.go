package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type storageRequestManager struct {
	client    client.Client
	namespace string
}

func newStorageRequestManager(cl client.Client, namespace string) (*storageRequestManager, error) {
	return &storageRequestManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

// getStorageRequestHash generates a hash for a StorageRequest based
// on the MD5 hash of the StorageClaim name and storageConsumer UUID.
func getStorageRequestHash(consumerUUID, storageClaimName string) string {
	s := struct {
		StorageConsumerUUID string `json:"storageConsumerUUID"`
		StorageClaimName    string `json:"storageClaimName"`
	}{
		consumerUUID,
		storageClaimName,
	}

	requestName, err := json.Marshal(s)
	if err != nil {
		klog.Errorf("failed to marshal a name for a storage class request based on %v. %v", s, err)
		panic("failed to marshal storage class request name")
	}
	md5Sum := md5.Sum(requestName)
	return hex.EncodeToString(md5Sum[:16])
}

// getStorageRequestName generates a name for a StorageRequest resource.
func getStorageRequestName(consumerUUID, storageClaimName string) string {
	return fmt.Sprintf("storagerequest-%s", getStorageRequestHash(consumerUUID, storageClaimName))
}

// Create creates a new StorageRequest resource and returns the StorageRequest ID.
func (s *storageRequestManager) Create(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	storageClaimName,
	requestType,
	encryptionMethod,
	storageProfile string,
) error {
	consumerUUID := string(consumer.GetUID())
	generatedRequestName := getStorageRequestName(consumerUUID, storageClaimName)

	storageRequestObj := &ocsv1alpha1.StorageRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRequestName,
			Namespace: s.namespace,
			Labels: map[string]string{
				controllers.ConsumerUUIDLabel: consumerUUID,
				storageRequestNameLabel:       storageClaimName,
			},
		},
		Spec: ocsv1alpha1.StorageRequestSpec{
			Type:             requestType,
			EncryptionMethod: encryptionMethod,
			StorageProfile:   storageProfile,
		},
	}
	if consumer.GetUID() == "" {
		return fmt.Errorf("empty UID for consumer %q", consumerUUID)
	}

	gvk, err := apiutil.GVKForObject(consumer, s.client.Scheme())
	if err != nil {
		return fmt.Errorf("failed to get gvk for consumer %q. %w", consumerUUID, err)
	}

	storageRequestObj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         gvk.GroupVersion().String(),
			Kind:               gvk.Kind,
			UID:                consumer.GetUID(),
			Name:               consumer.GetName(),
			BlockOwnerDeletion: ptr.To(true),
		},
	})

	err = s.client.Create(ctx, storageRequestObj)
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create a StorageRequest named %q for consumer %q and request %q. %w", generatedRequestName, consumerUUID, storageClaimName, err)
		}
		newStorageRequestObj := &ocsv1alpha1.StorageRequest{}
		getErr := s.client.Get(ctx, client.ObjectKey{Name: generatedRequestName, Namespace: s.namespace}, newStorageRequestObj)
		if getErr != nil {
			klog.Errorf("failed to get a StorageRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClaimName, getErr)
			return err
		}
		// check if the StorageRequest is getting deleted.
		if newStorageRequestObj.DeletionTimestamp != nil {
			klog.Warningf("StorageRequest named %q for consumer %q and request %q is already created but is getting deleted", generatedRequestName, consumerUUID, storageClaimName)
			return err
		}
		// check if the input is different
		if !reflect.DeepEqual(storageRequestObj.Spec, newStorageRequestObj.Spec) {
			klog.Errorf("StorageRequest named %q for consumer %q and request %q is already exists with different spec (%v) but requested spec (%v)", generatedRequestName, consumerUUID, storageClaimName, storageRequestObj.Spec, newStorageRequestObj.Spec)
			return err
		}
	}

	klog.Infof("successfully created a StorageRequest resource %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClaimName)

	return nil
}

// Delete deletes the storagerequest resource using storageClaimName
// and consumerUUID.
func (s *storageRequestManager) Delete(ctx context.Context, consumerUUID, storageClaimName string) error {
	generatedRequestName := getStorageRequestName(consumerUUID, storageClaimName)
	storageRequestObj := &ocsv1alpha1.StorageRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRequestName,
			Namespace: s.namespace,
		},
	}

	foregroundDelete := metav1.DeletePropagationForeground
	deleteOption := client.DeleteOptions{
		PropagationPolicy: &foregroundDelete,
	}
	if err := s.client.Delete(ctx, storageRequestObj, &deleteOption); err != nil {
		if kerrors.IsNotFound(err) {
			klog.Warningf("StorageRequest %q not found for consumer %q and request %q", generatedRequestName, consumerUUID, storageClaimName)
			return nil
		}
		return fmt.Errorf("failed to delete StorageRequest %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClaimName, err)
	}

	klog.Infof("successfully deleted StorageRequest %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClaimName)

	return nil
}

// Get returns the StorageRequest resource using storageClaimName and consumerUUID.
func (s *storageRequestManager) Get(ctx context.Context, consumerUUID, storageClaimName string) (*ocsv1alpha1.StorageRequest, error) {
	generatedRequestName := getStorageRequestName(consumerUUID, storageClaimName)
	storageRequestObj := &ocsv1alpha1.StorageRequest{}
	err := s.client.Get(ctx, types.NamespacedName{Name: generatedRequestName, Namespace: s.namespace}, storageRequestObj)
	if err != nil {
		klog.Errorf("failed to get a StorageRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClaimName, err)
		return nil, err
	}

	return storageRequestObj, nil
}
