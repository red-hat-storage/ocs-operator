package ceph

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"sync"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ cache.Store = &SecretWatcher{}

// SecretWatcher watches the ocs-metrics-exporter-ceph-auth secret for changes
// and notifies registered callbacks when credentials are rotated.
type SecretWatcher struct {
	mu                sync.RWMutex
	currentKeyHash    string
	onChangeCallbacks []func()
	namespace         string
	secretName        string
}

// NewSecretWatcher creates a new SecretWatcher instance.
func NewSecretWatcher(opts *options.Options) *SecretWatcher {
	return &SecretWatcher{
		onChangeCallbacks: []func(){},
		namespace:         opts.AllowedNamespaces[0],
		secretName:        util.OcsMetricsExporterCephClientName,
	}
}

// RegisterOnChange registers a callback to be called when the secret changes.
func (s *SecretWatcher) RegisterOnChange(callback func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onChangeCallbacks = append(s.onChangeCallbacks, callback)
}

// hashUserKey computes the SHA256 hash of the userKey.
func hashUserKey(userKey []byte) string {
	hash := sha256.Sum256(userKey)
	return hex.EncodeToString(hash[:])
}

// notifyCallbacks invokes all registered callbacks.
func (s *SecretWatcher) notifyCallbacks() {
	for _, callback := range s.onChangeCallbacks {
		callback()
	}
}

// Add implements cache.Store interface.
func (s *SecretWatcher) Add(obj interface{}) error {
	return s.Update(obj)
}

// Update implements cache.Store interface.
func (s *SecretWatcher) Update(obj interface{}) error {
	secret, ok := obj.(*corev1.Secret)
	if !ok {
		klog.Errorf("SecretWatcher: unexpected object type %T", obj)
		return nil
	}

	if secret.Name != s.secretName {
		return nil
	}

	userKey, ok := secret.Data["userKey"]
	if !ok {
		klog.Warning("SecretWatcher: userKey not found in secret")
		return nil
	}

	newHash := hashUserKey(userKey)

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.currentKeyHash == "" {
		// First time seeing the secret, just store the hash
		s.currentKeyHash = newHash
		klog.Info("SecretWatcher: initial credential hash stored")
		return nil
	}

	if s.currentKeyHash != newHash {
		klog.Info("SecretWatcher: credential change detected, triggering reconnection")
		s.currentKeyHash = newHash
		s.notifyCallbacks()
	}

	return nil
}

// Delete implements cache.Store interface.
func (s *SecretWatcher) Delete(_ interface{}) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.currentKeyHash = ""
	klog.Info("SecretWatcher: secret deleted, clearing credential hash")
	return nil
}

// List implements cache.Store interface.
func (s *SecretWatcher) List() []interface{} {
	return nil
}

// ListKeys implements cache.Store interface.
func (s *SecretWatcher) ListKeys() []string {
	return nil
}

// Get implements cache.Store interface.
func (s *SecretWatcher) Get(_ interface{}) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

// GetByKey implements cache.Store interface.
func (s *SecretWatcher) GetByKey(_ string) (item interface{}, exists bool, err error) {
	return nil, false, nil
}

// Replace implements cache.Store interface.
func (s *SecretWatcher) Replace(list []interface{}, _ string) error {
	for _, obj := range list {
		if err := s.Update(obj); err != nil {
			return err
		}
	}
	return nil
}

// Resync implements cache.Store interface.
func (s *SecretWatcher) Resync() error {
	return nil
}

// CreateSecretListWatch creates a ListWatch for the ceph auth secret.
func CreateSecretListWatch(kubeClient clientset.Interface, namespace, secretName string) cache.ListerWatcher {
	return &cache.ListWatch{
		ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
			opts.FieldSelector = "metadata.name=" + secretName
			return kubeClient.CoreV1().Secrets(namespace).List(context.TODO(), opts)
		},
		WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
			opts.FieldSelector = "metadata.name=" + secretName
			return kubeClient.CoreV1().Secrets(namespace).Watch(context.TODO(), opts)
		},
	}
}
