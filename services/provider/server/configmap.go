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
	errTicketAlreadyExistsStorageClusterPeer = errors.New("onboarding ticket already used by another storageClusterPeer representation")
)

type ocsConfigMapManager struct {
	client       client.Client
	namespace    string
	nameByTicket map[string]string
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

	for _, configMap := range configMaps.Items {
		if ticket, ok := configMap.GetAnnotations()[TicketAnnotation]; ok {
			nameByTicket[ticket] = configMap.Name
		}
	}

	return &ocsConfigMapManager{
		client:       cl,
		namespace:    namespace,
		nameByTicket: nameByTicket,
	}, nil
}

// Create creates a new storageClusterPeer representation resource, updates the cache and returns the representation UID
func (c *ocsConfigMapManager) Create(ctx context.Context, ticket, name, storageClusterName, storageClusterNamespace string) error {

	c.mutex.RLock()
	if _, ok := c.nameByTicket[ticket]; ok {
		c.mutex.RUnlock()
		klog.Warning("onboarding ticket already in use")
		return errTicketAlreadyExistsStorageClusterPeer
	}
	c.mutex.RUnlock()

	configMapObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: storageClusterNamespace,
			Annotations: map[string]string{
				TicketAnnotation: ticket,
			},
		},
		Data: map[string]string{
			"storageClusterName": fmt.Sprintf("%s/%s", storageClusterNamespace, storageClusterName),
		},
	}

	err := c.client.Create(ctx, configMapObj)
	if err != nil {
		if kerrors.IsAlreadyExists(err) {
			klog.Warningf("storageClusterPeer representation %q already exists", name)
			return err
		}
		return fmt.Errorf("failed to create  storageClusterPeer representation %q. %v", configMapObj.Name, err)
	}

	c.mutex.Lock()
	c.nameByTicket[ticket] = configMapObj.Name
	c.mutex.Unlock()

	klog.Infof("successfully created storageClusterPeer representation %q", name)

	return nil
}

// Delete deletes the storageClusterPeer representation resource using UID and updates the cache
func (c *ocsConfigMapManager) Delete(ctx context.Context, name, namespace string) error {

	configMapObj := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
	}

	if err := c.client.Delete(ctx, configMapObj); err != nil {
		if kerrors.IsNotFound(err) {
			klog.Warningf("storageClusterPeer representation  %q not found.", configMapObj.Name)
			return nil
		}
		return fmt.Errorf("failed to delete storageClusterPeer representation  %q. %v", configMapObj.Name, err)
	}

	klog.Infof("successfully deleted storageClusterPeer representation %q", configMapObj.Name)

	return nil
}

func (c *ocsConfigMapManager) Enable(ctx context.Context, name, namespace string) error {
	// Get storage cluster peer representation resource using name
	configMapObj, err := c.GetByName(ctx, name, namespace)
	if err != nil {
		return err
	}

	configMapObj.Data["enable"] = "true"
	err = c.client.Update(ctx, configMapObj)
	if err != nil {
		klog.Errorf("failed to enable storageClusterPeer representation %v", err)
		return fmt.Errorf("failed to update storageClusterPeer representation %q. %v", configMapObj.Name, err)
	}

	klog.Infof("successfully Enabled the storageClusterPeer representation %q", configMapObj.Name)
	return nil
}

// GetByName returns a storageClusterPeer representation resource using the Name
func (c *ocsConfigMapManager) GetByName(ctx context.Context, representationName, namespace string) (*corev1.ConfigMap, error) {

	configMapObj := &corev1.ConfigMap{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: representationName, Namespace: namespace}, configMapObj); err != nil {
		klog.Errorf("Failed to get the storageClusterPeer representation configMap %s: %v", representationName, err)
		return nil, err
	}

	return configMapObj, nil
}

func (c *ocsConfigMapManager) IsEnabled(configMap *corev1.ConfigMap) bool {
	return configMap.Data["enable"] == "true"
}
