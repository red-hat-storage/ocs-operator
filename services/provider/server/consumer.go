package server

import (
	"context"
	"fmt"
	"sync"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ocsConsumerManager struct {
	client    client.Client
	namespace string
	nameByUID map[types.UID]string
	mutex     sync.RWMutex
}

func newConsumerManager(ctx context.Context, cl client.Client, namespace string) (*ocsConsumerManager, error) {
	consumers := &ocsv1alpha1.StorageConsumerList{}
	err := cl.List(ctx, consumers, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storage consumers. %v", err)
	}

	nameByUID := map[types.UID]string{}

	for _, consumer := range consumers.Items {
		nameByUID[consumer.UID] = consumer.Name
	}

	return &ocsConsumerManager{
		client:    cl,
		namespace: namespace,
		nameByUID: nameByUID,
	}, nil
}

// EnableStorageConsumer enables storageConsumer resource
func (c *ocsConsumerManager) EnableStorageConsumer(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) (string, error) {
	consumer.Spec.Enable = true
	// update here acts as a synchronization point even if two api calls
	// resolves to a single storageconsumer the resourceVersion of one of
	// the calls will not match and be dropped
	if err := c.client.Update(ctx, consumer); err != nil {
		klog.Errorf("Failed to enable storageConsumer %v", err)
		return "", fmt.Errorf("failed to update storageConsumer resource %q. %v", consumer.Name, err)
	}

	c.mutex.Lock()
	c.nameByUID[consumer.UID] = consumer.Name
	c.mutex.Unlock()
	klog.Infof("successfully Enabled the StorageConsumer resource %q", consumer.Name)

	return string(consumer.UID), nil
}

// DisableStorageConsumer disable storageConsumer resource
func (c *ocsConsumerManager) DisableStorageConsumer(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) error {
	consumer.Spec.Enable = false
	// update here acts as a synchronization point even if two api calls
	// resolves to a single storageconsumer the resourceVersion of one of
	// the calls will not match and be dropped
	if err := c.client.Update(ctx, consumer); err != nil {
		klog.Errorf("Failed to disable storageConsumer %v", err)
		return fmt.Errorf("failed to update storageConsumer resource %q. %v", consumer.Name, err)
	}

	c.mutex.Lock()
	delete(c.nameByUID, consumer.UID)
	c.mutex.Unlock()
	klog.Infof("successfully Disabled the StorageConsumer resource %q", consumer.Name)

	return nil
}

// GetByName returns a storageConsumer resource using the Name
func (c *ocsConsumerManager) GetByName(ctx context.Context, name string) (*ocsv1alpha1.StorageConsumer, error) {

	consumerObj := &ocsv1alpha1.StorageConsumer{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: name, Namespace: c.namespace}, consumerObj); err != nil {
		klog.Errorf("Failed to get the storageConsumer %s: %v", name, err)
		return nil, err
	}

	return consumerObj, nil
}

// Get returns a storageConsumer resource using the UID
func (c *ocsConsumerManager) Get(ctx context.Context, id string) (*ocsv1alpha1.StorageConsumer, error) {
	uid := types.UID(id)

	c.mutex.RLock()
	consumerName, ok := c.nameByUID[uid]
	if !ok {
		c.mutex.RUnlock()
		klog.Errorf("no storageConsumer found with the UID %q", id)
		return nil, fmt.Errorf("no storageConsumer found with the UID %q", id)
	}
	c.mutex.RUnlock()

	consumerObj := &ocsv1alpha1.StorageConsumer{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: consumerName, Namespace: c.namespace}, consumerObj); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			return nil, fmt.Errorf("storageConsumer resource %q not found. %v", consumerName, err)
		}
		return nil, fmt.Errorf("failed to get storageConsumer resource with name %q. %v", consumerName, err)
	}

	return consumerObj, nil
}

func (c *ocsConsumerManager) UpdateConsumerStatus(ctx context.Context, id string, status ifaces.StorageClientStatus) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	consumerObj.Status.LastHeartbeat = metav1.Now()
	consumerObj.Status.Client.PlatformVersion = status.GetPlatformVersion()
	consumerObj.Status.Client.OperatorVersion = status.GetOperatorVersion()
	consumerObj.Status.Client.OperatorNamespace = status.GetOperatorNamespace()
	consumerObj.Status.Client.ClusterID = status.GetClusterID()
	consumerObj.Status.Client.Name = status.GetClientName()
	consumerObj.Status.Client.ID = status.GetClientID()
	consumerObj.Status.Client.ClusterName = status.GetClusterName()
	consumerObj.Status.Client.StorageQuotaUtilizationRatio = status.GetStorageQuotaUtilizationRatio()

	if err := c.client.Status().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("Failed to patch Status for StorageConsumer %v: %v", consumerObj.Name, err)
	}
	klog.Infof("successfully updated Status for StorageConsumer %v", consumerObj.Name)
	return nil
}

func (c *ocsConsumerManager) AddAnnotation(ctx context.Context, id string, annotation, value string) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	if util.AddAnnotation(consumerObj, annotation, value) {
		if err = c.client.Update(ctx, consumerObj); err != nil {
			return fmt.Errorf(
				"failed to add annotation %s to StorageConsumer %v: %v",
				annotation,
				consumerObj.Name,
				err,
			)
		}
	}
	return nil
}

func (c *ocsConsumerManager) RemoveAnnotation(ctx context.Context, id string, annotation string) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	_, hasAnnotation := consumerObj.GetAnnotations()[annotation]
	if hasAnnotation {
		delete(consumerObj.GetAnnotations(), annotation)
		if err = c.client.Update(ctx, consumerObj); err != nil {
			return fmt.Errorf(
				"failed to remove annotation %s from StorageConsumer %v: %v",
				annotation,
				consumerObj.Name,
				err,
			)
		}
	}
	return nil
}

func (c *ocsConsumerManager) GetByClientID(ctx context.Context, clientID string) (*ocsv1alpha1.StorageConsumer, error) {
	consumerObjList := &ocsv1alpha1.StorageConsumerList{}
	err := c.client.List(ctx, consumerObjList, client.InNamespace(c.namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storageConsumer objects: %v", err)
	}
	for i := range consumerObjList.Items {
		consumer := consumerObjList.Items[i]
		if consumer.Status.Client.ID == clientID {
			return &consumer, nil
		}
	}
	return nil, nil
}
