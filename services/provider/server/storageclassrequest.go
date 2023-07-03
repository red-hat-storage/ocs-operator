package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

type storageClassRequestManager struct {
	client    client.Client
	namespace string
}

func newStorageClassRequestManager(ctx context.Context, cl client.Client, namespace string) (*storageClassRequestManager, error) {
	return &storageClassRequestManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

// getStorageClassRequestName generates a name for a StorageClassRequest resource.
func getStorageClassRequestName(consumerUUID, storageClassRequestName string) string {
	var s struct {
		StorageConsumerUUID     string `json:"storageConsumerUUID"`
		StorageClassRequestName string `json:"storageClassRequestName"`
	}
	s.StorageConsumerUUID = consumerUUID
	s.StorageClassRequestName = storageClassRequestName

	requestName, err := json.Marshal(s)
	if err != nil {
		klog.Errorf("failed to marshal a name for a storage class request based on %v. %v", s, err)
		panic("failed to marshal storage class request name")
	}
	name := md5.Sum([]byte(requestName))
	// The name of the StorageClassRequest is the MD5 hash of the JSON
	// representation of the StorageClassRequest name and storageConsumer UUID.
	return fmt.Sprintf("storageclassrequest-%s", hex.EncodeToString(name[:16]))
}

// Create creates a new StorageClassRequest resource and returns the StorageClassRequest ID.
func (s *storageClassRequestManager) Create(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	storageClassRequestName,
	requestType,
	encryptionMethod,
	storageProfile string,
) error {
	consumerUUID := string(consumer.GetUID())
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassRequestName)

	storageClassRequestObj := &ocsv1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generatedRequestName,
			Namespace: s.namespace,
			Labels: map[string]string{
				controllers.ConsumerUUIDLabel: consumerUUID,
				storageClassRequestNameLabel:  storageClassRequestName,
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
			BlockOwnerDeletion: pointer.Bool(true),
		},
	})

	err = s.client.Create(ctx, storageClassRequestObj)
	if err != nil {
		if !kerrors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create a StorageClassRequest named %q for consumer %q and request %q. %w", generatedRequestName, consumerUUID, storageClassRequestName, err)
		}
		newStorageClassRequestObj := &ocsv1alpha1.StorageClassRequest{}
		getErr := s.client.Get(ctx, client.ObjectKey{Name: generatedRequestName, Namespace: s.namespace}, newStorageClassRequestObj)
		if getErr != nil {
			klog.Errorf("failed to get a StorageClassRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassRequestName, getErr)
			return err
		}
		// check if the StorageClassRequest is getting deleted.
		if newStorageClassRequestObj.DeletionTimestamp != nil {
			klog.Warningf("StorageClassRequest named %q for consumer %q and request %q is already created but is getting deleted", generatedRequestName, consumerUUID, storageClassRequestName)
			return err
		}
		// check if the input is different
		if !reflect.DeepEqual(storageClassRequestObj.Spec, newStorageClassRequestObj.Spec) {
			klog.Errorf("StorageClassRequest named %q for consumer %q and request %q is already exists with different spec (%v) but requested spec (%v)", generatedRequestName, consumerUUID, storageClassRequestName, storageClassRequestObj.Spec, newStorageClassRequestObj.Spec)
			return err
		}
	}

	klog.Infof("successfully created a StorageClassRequest resource %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassRequestName)

	return nil
}

// Delete deletes the storageclassrequest resource using storageClassRequestName
// and consumerUUID.
func (s *storageClassRequestManager) Delete(ctx context.Context, consumerUUID, storageClassRequestName string) error {
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassRequestName)
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
			klog.Warningf("StorageClassRequest %q not found for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassRequestName)
			return nil
		}
		return fmt.Errorf("failed to delete StorageClassRequest %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassRequestName, err)
	}

	klog.Infof("successfully deleted StorageClassRequest %q for consumer %q and request %q", generatedRequestName, consumerUUID, storageClassRequestName)

	return nil
}

// Get returns the StorageClassRequest resource using storageClassRequestName
// and consumerUUID.
func (s *storageClassRequestManager) Get(ctx context.Context, consumerUUID, storageClassRequestName string) (*ocsv1alpha1.StorageClassRequest, error) {
	generatedRequestName := getStorageClassRequestName(consumerUUID, storageClassRequestName)
	storageClassRequestObj := &ocsv1alpha1.StorageClassRequest{}
	err := s.client.Get(ctx, types.NamespacedName{Name: generatedRequestName, Namespace: s.namespace}, storageClassRequestObj)
	if err != nil {
		klog.Errorf("failed to get a StorageClassRequest named %q for consumer %q and request %q. %v", generatedRequestName, consumerUUID, storageClassRequestName, err)
		return nil, err
	}

	return storageClassRequestObj, nil
}
