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

type storageClassRequestManager struct {
	client    client.Client
	namespace string
}

func newStorageClassRequestManager(cl client.Client, namespace string) (*storageClassRequestManager, error) {
	return &storageClassRequestManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

// getStorageClassRequestHash generates a hash for a StorageClassRequest based
// on the MD5 hash of the StorageClassClaim name and storageConsumer UUID.
func getStorageClassRequestHash(consumerUUID, storageClassClaimName string) string {
	var s struct {
		StorageConsumerUUID string `json:"storageConsumerUUID"`

		// This field was named before a small refactor to clarify
		// what it actually represents, but is being kept as-is so as
		// to not change the generative logic.
		StorageClassClaimName string `json:"storageClassRequestName"`
	}
	s.StorageConsumerUUID = consumerUUID
	s.StorageClassClaimName = storageClassClaimName

	requestName, err := json.Marshal(s)
	if err != nil {
		klog.Errorf("failed to marshal a name for a storage class request based on %v. %v", s, err)
		panic("failed to marshal storage class request name")
	}
	hash := md5.Sum([]byte(requestName))
	return hex.EncodeToString(hash[:16])
}

// getStorageClassRequestName generates a name for a StorageClassRequest resource.
func getStorageClassRequestName(consumerUUID, storageClassClaimName string) string {
	return fmt.Sprintf("storageclassrequest-%s", getStorageClassRequestHash(consumerUUID, storageClassClaimName))
}

// Create creates a new StorageClassRequest resource and returns the StorageClassRequest ID.
func (s *storageClassRequestManager) Create(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	storageClassClaimName,
	requestType,
	encryptionMethod,
	storageProfile string,
) error {
	consumerUUID := string(consumer.GetUID())
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassClaimName)

	storageClassRequestObj := &ocsv1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRequestName,
			Namespace: s.namespace,
			Labels: map[string]string{
				controllers.ConsumerUUIDLabel: consumerUUID,
				storageClassClaimNameLabel:    storageClassClaimName,
			},
		},
		Spec: ocsv1alpha1.StorageClassRequestSpec{
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

	storageClassRequestObj.SetOwnerReferences([]metav1.OwnerReference{
		{
			APIVersion:         gvk.GroupVersion().String(),
			Kind:               gvk.Kind,
			UID:                consumer.GetUID(),
			Name:               consumer.GetName(),
			BlockOwnerDeletion: ptr.To(true),
		},
	})

	err = s.client.Create(ctx, storageClassRequestObj)
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create a StorageClassRequest named %q for consumer %q and request %q. %w", generatedRequestName, consumerUUID, storageClassClaimName, err)
		}
		newStorageClassRequestObj := &ocsv1alpha1.StorageClassRequest{}
		getErr := s.client.Get(ctx, client.ObjectKey{Name: generatedRequestName, Namespace: s.namespace}, newStorageClassRequestObj)
		if getErr != nil {
			klog.Errorf("failed to get a StorageClassRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassClaimName, getErr)
			return err
		}
		// check if the StorageClassRequest is getting deleted.
		if newStorageClassRequestObj.DeletionTimestamp != nil {
			klog.Warningf("StorageClassRequest named %q for consumer %q and request %q is already created but is getting deleted", generatedRequestName, consumerUUID, storageClassClaimName)
			return err
		}
		// check if the input is different
		if !reflect.DeepEqual(storageClassRequestObj.Spec, newStorageClassRequestObj.Spec) {
			klog.Errorf("StorageClassRequest named %q for consumer %q and request %q is already exists with different spec (%v) but requested spec (%v)", generatedRequestName, consumerUUID, storageClassClaimName, storageClassRequestObj.Spec, newStorageClassRequestObj.Spec)
			return err
		}
	}

	klog.Infof("successfully created a StorageClassRequest resource %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassClaimName)

	return nil
}

// Delete deletes the storageclassrequest resource using storageClassClaimName
// and consumerUUID.
func (s *storageClassRequestManager) Delete(ctx context.Context, consumerUUID, storageClassClaimName string) error {
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassClaimName)
	storageClassRequestObj := &ocsv1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRequestName,
			Namespace: s.namespace,
		},
	}

	foregroundDelete := metav1.DeletePropagationForeground
	deleteOption := client.DeleteOptions{
		PropagationPolicy: &foregroundDelete,
	}
	if err := s.client.Delete(ctx, storageClassRequestObj, &deleteOption); err != nil {
		if kerrors.IsNotFound(err) {
			klog.Warningf("StorageClassRequest %q not found for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassClaimName)
			return nil
		}
		return fmt.Errorf("failed to delete StorageClassRequest %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassClaimName, err)
	}

	klog.Infof("successfully deleted StorageClassRequest %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassClaimName)

	return nil
}

// Get returns the StorageClassRequest resource using a name generated from
// a given StorageClassClaim name and StorageConsumer UUID.
func (s *storageClassRequestManager) Get(ctx context.Context, consumerUUID, storageClassClaimName string) (*ocsv1alpha1.StorageClassRequest, error) {
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassClaimName)
	storageClassRequestObj := &ocsv1alpha1.StorageClassRequest{}
	err := s.client.Get(ctx, types.NamespacedName{Name: generatedRequestName, Namespace: s.namespace}, storageClassRequestObj)
	if err != nil {
		klog.Errorf("failed to get a StorageClassRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassClaimName, err)
		return nil, err
	}

	return storageClassRequestObj, nil
}
