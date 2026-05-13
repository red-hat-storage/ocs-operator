package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
)

var _ = Describe("SecretWatcher", func() {
	var opts *options.Options
	var secretWatcher *SecretWatcher

	BeforeEach(func() {
		opts = &options.Options{
			Kubeconfig:        &rest.Config{},
			AllowedNamespaces: []string{"openshift-storage"},
			CephAuthNamespace: "openshift-storage",
		}
		secretWatcher = NewSecretWatcher(opts)
	})

	Describe("NewSecretWatcher", func() {
		It("should create a new SecretWatcher", func() {
			Expect(secretWatcher).ToNot(BeNil())
			Expect(secretWatcher.namespace).To(Equal("openshift-storage"))
			Expect(secretWatcher.secretName).To(Equal("ocs-metrics-exporter-ceph-auth"))
			Expect(secretWatcher.onChangeCallbacks).To(BeEmpty())
		})
	})

	Describe("RegisterOnChange", func() {
		It("should register a callback", func() {
			callbackCalled := false
			callback := func() {
				callbackCalled = true
			}

			secretWatcher.RegisterOnChange(callback)
			Expect(len(secretWatcher.onChangeCallbacks)).To(Equal(1))

			// Trigger the callback
			secretWatcher.onChangeCallbacks[0]()
			Expect(callbackCalled).To(BeTrue())
		})

		It("should register multiple callbacks", func() {
			callback1Called := false
			callback2Called := false

			secretWatcher.RegisterOnChange(func() { callback1Called = true })
			secretWatcher.RegisterOnChange(func() { callback2Called = true })

			Expect(len(secretWatcher.onChangeCallbacks)).To(Equal(2))

			// Trigger all callbacks
			for _, cb := range secretWatcher.onChangeCallbacks {
				cb()
			}
			Expect(callback1Called).To(BeTrue())
			Expect(callback2Called).To(BeTrue())
		})
	})

	Describe("Update", func() {
		When("secret has different name", func() {
			It("should not process the secret", func() {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "different-secret",
						Namespace: "openshift-storage",
					},
					Data: map[string][]byte{
						"userKey": []byte("test-key"),
					},
				}

				err := secretWatcher.Update(secret)
				Expect(err).To(BeNil())
				Expect(secretWatcher.currentKeyHash).To(BeEmpty())
			})
		})

		When("secret is missing userKey", func() {
			It("should not update the hash", func() {
				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ocs-metrics-exporter-ceph-auth",
						Namespace: "openshift-storage",
					},
					Data: map[string][]byte{
						"userID": []byte("test-id"),
					},
				}

				err := secretWatcher.Update(secret)
				Expect(err).To(BeNil())
				Expect(secretWatcher.currentKeyHash).To(BeEmpty())
			})
		})

		When("secret is valid and first update", func() {
			It("should store the initial hash without triggering callbacks", func() {
				callbackCalled := false
				secretWatcher.RegisterOnChange(func() { callbackCalled = true })

				secret := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ocs-metrics-exporter-ceph-auth",
						Namespace: "openshift-storage",
					},
					Data: map[string][]byte{
						"userKey": []byte("initial-key"),
					},
				}

				err := secretWatcher.Update(secret)
				Expect(err).To(BeNil())
				Expect(secretWatcher.currentKeyHash).ToNot(BeEmpty())
				Expect(callbackCalled).To(BeFalse())
			})
		})

		When("secret key changes", func() {
			It("should trigger registered callbacks", func() {
				callbackCalled := false
				secretWatcher.RegisterOnChange(func() { callbackCalled = true })

				// First update - initial key
				secret1 := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ocs-metrics-exporter-ceph-auth",
						Namespace: "openshift-storage",
					},
					Data: map[string][]byte{
						"userKey": []byte("initial-key"),
					},
				}
				err := secretWatcher.Update(secret1)
				Expect(err).To(BeNil())
				Expect(callbackCalled).To(BeFalse())

				initialHash := secretWatcher.currentKeyHash

				// Second update - same key
				err = secretWatcher.Update(secret1)
				Expect(err).To(BeNil())
				Expect(callbackCalled).To(BeFalse())
				Expect(secretWatcher.currentKeyHash).To(Equal(initialHash))

				// Third update - different key
				secret2 := &corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "ocs-metrics-exporter-ceph-auth",
						Namespace: "openshift-storage",
					},
					Data: map[string][]byte{
						"userKey": []byte("rotated-key"),
					},
				}
				err = secretWatcher.Update(secret2)
				Expect(err).To(BeNil())
				Expect(callbackCalled).To(BeTrue())
				Expect(secretWatcher.currentKeyHash).ToNot(Equal(initialHash))
			})
		})
	})

	Describe("Add", func() {
		It("should delegate to Update", func() {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "ocs-metrics-exporter-ceph-auth",
					Namespace: "openshift-storage",
				},
				Data: map[string][]byte{
					"userKey": []byte("test-key"),
				},
			}

			err := secretWatcher.Add(secret)
			Expect(err).To(BeNil())
			Expect(secretWatcher.currentKeyHash).ToNot(BeEmpty())
		})
	})

	Describe("Delete", func() {
		It("should clear the hash", func() {
			// First set a hash
			secretWatcher.currentKeyHash = "some-hash"

			err := secretWatcher.Delete(nil)
			Expect(err).To(BeNil())
			Expect(secretWatcher.currentKeyHash).To(BeEmpty())
		})
	})

	Describe("Replace", func() {
		It("should update with items from the list", func() {
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
			err := secretWatcher.Replace(list, "")
			Expect(err).To(BeNil())
			Expect(secretWatcher.currentKeyHash).ToNot(BeEmpty())
		})
	})

	Describe("hashUserKey", func() {
		It("should produce consistent hashes for the same input", func() {
			key := []byte("test-key")
			hash1 := hashUserKey(key)
			hash2 := hashUserKey(key)
			Expect(hash1).To(Equal(hash2))
		})

		It("should produce different hashes for different inputs", func() {
			hash1 := hashUserKey([]byte("key1"))
			hash2 := hashUserKey([]byte("key2"))
			Expect(hash1).ToNot(Equal(hash2))
		})
	})
})
