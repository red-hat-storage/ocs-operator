package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	ifaces "github.com/red-hat-storage/ocs-operator/v4/services/provider/interfaces"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errTicketAlreadyExists = errors.New("onboarding ticket already used by another storageConsumer")
)

type ocsConsumerManager struct {
	client       client.Client
	namespace    string
	nameByTicket map[string]string
	nameByUID    map[types.UID]string
	mutex        sync.RWMutex
}

func newConsumerManager(ctx context.Context, cl client.Client, namespace string) (*ocsConsumerManager, error) {
	consumers := &ocsv1alpha1.StorageConsumerList{}
	err := cl.List(ctx, consumers, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storage consumers. %v", err)
	}

	nameByTicket := map[string]string{}
	nameByUID := map[types.UID]string{}

	for _, consumer := range consumers.Items {
		nameByUID[consumer.UID] = consumer.Name

		if ticket, ok := consumer.GetAnnotations()[TicketAnnotation]; ok {
			nameByTicket[ticket] = consumer.Name
		}
	}

	return &ocsConsumerManager{
		client:       cl,
		namespace:    namespace,
		nameByTicket: nameByTicket,
		nameByUID:    nameByUID,
	}, nil
}

// Create creates a new storageConsumer resource, updates the consumer cache and returns the storageConsumer UID
func (c *ocsConsumerManager) Create(ctx context.Context, onboard ifaces.StorageClientOnboarding) (string, error) {
	ticket := onboard.GetOnboardingTicket()
	name := onboard.GetConsumerName()
	c.mutex.RLock()
	if _, ok := c.nameByTicket[ticket]; ok {
		c.mutex.RUnlock()
		klog.Warning("onboarding ticket already in use")
		return "", errTicketAlreadyExists
	}
	c.mutex.RUnlock()

	consumerObj := &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
			Annotations: map[string]string{
				TicketAnnotation: ticket,
			},
		},
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: false,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			Client: ocsv1alpha1.ClientStatus{
				OperatorVersion: onboard.GetClientOperatorVersion(),
			},
		},
	}

	err := c.client.Create(ctx, consumerObj)
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			klog.Warningf("storageConsumer %q already exists", name)
			return "", err
		}
		return "", fmt.Errorf("failed to create storageConsumer resource %q. %v", consumerObj.Name, err)
	}

	c.mutex.Lock()
	c.nameByUID[consumerObj.UID] = consumerObj.Name
	c.nameByTicket[ticket] = consumerObj.Name
	c.mutex.Unlock()

	klog.Infof("successfully created storageConsumer resource %q", name)

	return string(consumerObj.UID), nil
}

// Delete deletes the storageConsumer resource using UID and updates the consumer cache
func (c *ocsConsumerManager) Delete(ctx context.Context, id string) error {
	uid := types.UID(id)
	c.mutex.RLock()
	consumerName, ok := c.nameByUID[uid]
	if !ok {
		klog.Warningf("no storageConsumer found with UID %q", id)
		c.mutex.RUnlock()
		return nil
	}
	c.mutex.RUnlock()

	consumerObj := &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consumerName,
			Namespace: c.namespace,
		},
	}

	foregroundDelete := metav1.DeletePropagationForeground
	deleteOption := client.DeleteOptions{
		PropagationPolicy: &foregroundDelete,
	}
	if err := c.client.Delete(ctx, consumerObj, &deleteOption); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			klog.Warningf("storageConsumer %q not found.", consumerObj.Name)
			return nil
		}
		return fmt.Errorf("failed to delete storageConsumer %q. %v", consumerName, err)
	}

	c.mutex.Lock()
	delete(c.nameByUID, uid)
	c.mutex.Unlock()

	klog.Infof("successfully deleted storageConsumer resource %q", consumerName)

	return nil
}

// EnableStorageConsumer enables storageConsumer resource
func (c *ocsConsumerManager) EnableStorageConsumer(ctx context.Context, id string) error {
	// Get storage consumer resource using UID
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	consumerObj.Spec.Enable = true
	err = c.client.Update(ctx, consumerObj)
	if err != nil {
		klog.Errorf("Failed to enable storageConsumer %v", err)
		return fmt.Errorf("failed to update storageConsumer resource %q. %v", consumerObj.Name, err)
	}

	klog.Infof("successfully Enabled the StorageConsumer resource %q", consumerObj.Name)

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

	if err := c.client.Status().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("Failed to patch Status for StorageConsumer %v: %v", consumerObj.Name, err)
	}
	klog.Infof("successfully updated Status for StorageConsumer %v", consumerObj.Name)
	return nil
}
