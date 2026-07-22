package ceph

import (
	"testing"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

func TestNewSecretWatcher(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	if watcher == nil {
		t.Fatal("expected non-nil watcher")
	}
	if watcher.namespace != "openshift-storage" {
		t.Errorf("expected namespace 'openshift-storage', got %q", watcher.namespace)
	}
	if watcher.secretName != "ocs-metrics-exporter-ceph-auth" {
		t.Errorf("expected secretName 'ocs-metrics-exporter-ceph-auth', got %q", watcher.secretName)
	}
	if len(watcher.onChangeCallbacks) != 0 {
		t.Errorf("expected empty callbacks, got %d", len(watcher.onChangeCallbacks))
	}
}

func TestRegisterOnChange(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	callCount := 0
	callback := func() {
		callCount++
	}

	watcher.RegisterOnChange(callback)
	if len(watcher.onChangeCallbacks) != 1 {
		t.Errorf("expected 1 callback, got %d", len(watcher.onChangeCallbacks))
	}

	// Trigger the callback manually
	watcher.onChangeCallbacks[0]()
	if callCount != 1 {
		t.Errorf("expected callCount 1, got %d", callCount)
	}
}

func TestRegisterMultipleCallbacks(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	callback1Called := false
	callback2Called := false

	watcher.RegisterOnChange(func() { callback1Called = true })
	watcher.RegisterOnChange(func() { callback2Called = true })

	if len(watcher.onChangeCallbacks) != 2 {
		t.Errorf("expected 2 callbacks, got %d", len(watcher.onChangeCallbacks))
	}

	// Trigger all callbacks
	for _, cb := range watcher.onChangeCallbacks {
		cb()
	}
	if !callback1Called {
		t.Error("expected callback1 to be called")
	}
	if !callback2Called {
		t.Error("expected callback2 to be called")
	}
}

func TestUpdateWithDifferentSecretName(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "different-secret",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("test-key"),
		},
	}

	err := watcher.Update(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash != "" {
		t.Errorf("expected empty hash, got %q", watcher.currentKeyHash)
	}
}

func TestUpdateWithMissingUserKey(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userID": []byte("test-id"),
		},
	}

	err := watcher.Update(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash != "" {
		t.Errorf("expected empty hash, got %q", watcher.currentKeyHash)
	}
}

func TestUpdateInitialSecret(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	callbackCalled := false
	watcher.RegisterOnChange(func() { callbackCalled = true })

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("initial-key"),
		},
	}

	err := watcher.Update(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash == "" {
		t.Error("expected non-empty hash after initial update")
	}
	if callbackCalled {
		t.Error("callback should not be called on initial update")
	}
}

func TestUpdateSameKey(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	callbackCalled := false
	watcher.RegisterOnChange(func() { callbackCalled = true })

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("test-key"),
		},
	}

	// First update
	_ = watcher.Update(secret)
	initialHash := watcher.currentKeyHash

	// Second update with same key
	err := watcher.Update(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash != initialHash {
		t.Error("hash should not change for same key")
	}
	if callbackCalled {
		t.Error("callback should not be called when key unchanged")
	}
}

func TestUpdateKeyRotation(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	callbackCalled := false
	watcher.RegisterOnChange(func() { callbackCalled = true })

	// Initial secret
	secret1 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("initial-key"),
		},
	}
	_ = watcher.Update(secret1)
	initialHash := watcher.currentKeyHash

	// Rotated secret
	secret2 := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("rotated-key"),
		},
	}
	err := watcher.Update(secret2)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash == initialHash {
		t.Error("hash should change after key rotation")
	}
	if !callbackCalled {
		t.Error("callback should be called on key rotation")
	}
}

func TestAdd(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("test-key"),
		},
	}

	err := watcher.Add(secret)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash == "" {
		t.Error("expected non-empty hash after Add")
	}
}

func TestDelete(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)
	watcher.currentKeyHash = "some-hash"

	err := watcher.Delete(nil)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash != "" {
		t.Errorf("expected empty hash after Delete, got %q", watcher.currentKeyHash)
	}
}

func TestReplace(t *testing.T) {
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{"openshift-storage"},
	}
	watcher := NewSecretWatcher(opts)

	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-metrics-exporter-ceph-auth",
			Namespace: "openshift-storage",
		},
		Data: map[string][]byte{
			"userKey": []byte("test-key"),
		},
	}

	list := []interface{}{secret}
	err := watcher.Replace(list, "")
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if watcher.currentKeyHash == "" {
		t.Error("expected non-empty hash after Replace")
	}
}

func TestHashUserKey(t *testing.T) {
	key := []byte("test-key")
	hash1 := hashUserKey(key)
	hash2 := hashUserKey(key)

	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}

	hash3 := hashUserKey([]byte("different-key"))
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}
}
