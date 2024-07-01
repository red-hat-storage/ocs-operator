package server

import (
	"context"
	"errors"
	"fmt"
	"sync"

	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	errTicketAlreadyExistsStorageClusterPeer = errors.New("onboarding ticket already used by another storageClusterPeer")
)

type ocsConfigMapManager struct {
	client       client.Client
	namespace    string
	nameByTicket map[string]string
	nameByUID    map[types.UID]string
	mutex        sync.RWMutex
}

func newConfigMapManager(ctx context.Context, cl client.Client, namespace string) (*ocsConfigMapManager, error) {
	configMaps := &corev1.ConfigMapList{}

	//TODO: figure out a way to list only the configMaps for storageClusterPeer Representation
	// we can use cached client to set up a indexer based on ticket annotation or prefix or use a label?
	err := cl.List(ctx, configMaps, client.InNamespace(namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storage consumers. %v", err)
	}

	nameByTicket := map[string]string{}
	nameByUID := map[types.UID]string{}

	for _, configMap := range configMaps.Items {
		if ticket, ok := configMap.GetAnnotations()[TicketAnnotation]; ok {
			nameByUID[configMap.UID] = configMap.Name
			nameByTicket[ticket] = configMap.Name
		}
	}

	return &ocsConfigMapManager{
		client:       cl,
		namespace:    namespace,
		nameByTicket: nameByTicket,
		nameByUID:    nameByUID,
	}, nil
}

// Create creates a new storageClusterPeer representation resource, updates the cache and returns the representation UID
func (c *ocsConfigMapManager) Create(ctx context.Context, ticket, name string) (string, error) {

	c.mutex.RLock()
	if _, ok := c.nameByTicket[ticket]; ok {
		c.mutex.RUnlock()
		klog.Warning("onboarding ticket already in use")
		return "", errTicketAlreadyExistsStorageClusterPeer
	}
	c.mutex.RUnlock()

	configMapObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: c.namespace,
			Annotations: map[string]string{
				TicketAnnotation: ticket,
			},
		},
	}

	err := c.client.Create(ctx, configMapObj)
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			klog.Warningf("storageClusterPeer representation %q already exists", name)
			return "", err
		}
		return "", fmt.Errorf("failed to create  storageClusterPeer representation %q. %v", configMapObj.Name, err)
	}

	c.mutex.Lock()
	c.nameByUID[configMapObj.UID] = configMapObj.Name
	c.nameByTicket[ticket] = configMapObj.Name
	c.mutex.Unlock()

	klog.Infof("successfully created storageClusterPeer representation %q", name)

	return string(configMapObj.UID), nil
}

// Delete deletes the storageClusterPeer representation resource using UID and updates the cache
func (c *ocsConfigMapManager) Delete(ctx context.Context, id string) error {
	uid := types.UID(id)
	c.mutex.RLock()
	representationName, ok := c.nameByUID[uid]
	if !ok {
		klog.Warningf("no storageClusterPeer representation found with UID %q", id)
		c.mutex.RUnlock()
		return nil
	}
	c.mutex.RUnlock()

	configMapObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      representationName,
			Namespace: c.namespace,
		},
	}

	if err := c.client.Delete(ctx, configMapObj); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			klog.Warningf("storageClusterPeer representation  %q not found.", configMapObj.Name)
			return nil
		}
		return fmt.Errorf("failed to delete storageClusterPeer representation  %q. %v", configMapObj.Name, err)
	}

	c.mutex.Lock()
	delete(c.nameByUID, uid)
	c.mutex.Unlock()

	klog.Infof("successfully deleted storageClusterPeer representation %q", configMapObj.Name)

	return nil
}

func (c *ocsConfigMapManager) Enable(ctx context.Context, id string) error {
	// Get storage consumer resource using UID
	configMapObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	if configMapObj.Data == nil {
		configMapObj.Data = map[string]string{}
	}

	configMapObj.Data["enable"] = "true"
	err = c.client.Update(ctx, configMapObj)
	if err != nil {
		klog.Errorf("Failed to enable storageClusterPeer representation %v", err)
		return fmt.Errorf("failed to update storageClusterPeer representation %q. %v", configMapObj.Name, err)
	}

	klog.Infof("successfully Enabled the storageClusterPeer representation %q", configMapObj.Name)
	return nil
}

// GetByName returns a storageClusterPeer representation resource using the Name
func (c *ocsConfigMapManager) GetByName(ctx context.Context, representationName string) (*corev1.ConfigMap, error) {

	configMapObj := &corev1.ConfigMap{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: representationName, Namespace: c.namespace}, configMapObj); err != nil {
		klog.Errorf("Failed to get the storageClusterPeer representation configMap %s: %v", representationName, err)
		return nil, err
	}

	return configMapObj, nil
}

// Get returns a storageClusterPeer representation resource using the UID
func (c *ocsConfigMapManager) Get(ctx context.Context, id string) (*corev1.ConfigMap, error) {
	uid := types.UID(id)

	c.mutex.RLock()
	representationName, ok := c.nameByUID[uid]
	if !ok {
		c.mutex.RUnlock()
		klog.Errorf("no storageClusterPeer representation found with the UID %q", id)
		return nil, fmt.Errorf("no storageClusterPeer representation found with the UID %q", id)
	}
	c.mutex.RUnlock()

	consumerObj := &corev1.ConfigMap{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: representationName, Namespace: c.namespace}, consumerObj); err != nil {
		if kerrors.IsNotFound(err) {
			// update uidStore
			c.mutex.Lock()
			delete(c.nameByUID, uid)
			c.mutex.Unlock()
			return nil, fmt.Errorf("storageClusterPeer representation resource %q not found. %v", representationName, err)
		}
		return nil, fmt.Errorf("failed to get storageClusterPeer representation resource with name %q. %v", representationName, err)
	}

	return consumerObj, nil
}
