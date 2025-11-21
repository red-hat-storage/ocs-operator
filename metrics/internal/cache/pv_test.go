package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var _ = Describe("PersistentVolume Cache", func() {
	defer GinkgoRecover()
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{""},
		CephAuthNamespace: "",
	}

	When("new cache is requested", func() {
		It("should return a new empty PersistentVolume cache", func() {
			pvStore := NewPersistentVolumeStore(opts)
			Expect(pvStore).ToNot(BeNil())
			Expect(pvStore.Store).To(BeEmpty())
		})
	})

	When("PV is added", func() {
		pvStore := NewPersistentVolumeStore(opts)
		pvStore.initCephFn = func(kubeclient kubernetes.Interface, cephClusterNamespace, cephAuthNamespace string) (cephMonitorConfig, error) {
			return cephMonitorConfig{}, nil
		}
		pvStore.runCephRBDStatusFn = func(config *cephMonitorConfig, pool, image, namespace string) (Clients, error) {
			return Clients{
				Watchers: []Watcher{
					{
						Address: "test-watcher-address",
					},
				},
			}, nil
		}
		pvStore.getNodeNameForPVFn = func(pv *corev1.PersistentVolume, kubeClient kubernetes.Interface) (string, error) {
			return "node-for-valid-pv", nil
		}

		When("PV is missing required fields", func() {
			It("should not add the PV to cache", func() {
				pv := corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-invalid",
					},
					Spec:   corev1.PersistentVolumeSpec{},
					Status: corev1.PersistentVolumeStatus{},
				}
				err := pvStore.Add(&pv)
				Expect(err).To(BeNil())
				_, exists, err := pvStore.Get(pv)
				Expect(err).To(BeNil())
				Expect(exists).To(BeFalse())
			})
		})

		When("PV has required fields", func() {
			It("should add the PV to cache", func() {
				pv := corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-valid",
						Annotations: map[string]string{
							"pv.kubernetes.io/provisioned-by": "cluster.rbd.csi.ceph.com",
						},
						UID: types.UID("uid"),
					},
					Spec: corev1.PersistentVolumeSpec{
						PersistentVolumeSource: corev1.PersistentVolumeSource{
							CSI: &corev1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									"imageName": "imagename",
									"pool":      "pool",
								},
							},
						},
						ClaimRef: &corev1.ObjectReference{
							Name:      "test-claimref",
							Namespace: "test-claimref",
						},
					},
					Status: corev1.PersistentVolumeStatus{},
				}
				err := pvStore.Add(&pv)
				Expect(err).To(BeNil())
				_, exists, err := pvStore.Get(&corev1.PersistentVolume{})
				Expect(err).To(BeNil())
				Expect(exists).To(BeFalse()) // get always returns exists=false
			})
		})
		When("PV is a CephFS PV", func() {
			It("should add the PV to CephFSPVList and extract FSName", func() {
				pv := corev1.PersistentVolume{
					ObjectMeta: metav1.ObjectMeta{
						Name: "test-cephfs-pv",
						Annotations: map[string]string{
							"pv.kubernetes.io/provisioned-by": "cluster.cephfs.csi.ceph.com",
						},
						UID: types.UID("cephfs-uid"),
					},
					Spec: corev1.PersistentVolumeSpec{
						PersistentVolumeSource: corev1.PersistentVolumeSource{
							CSI: &corev1.CSIPersistentVolumeSource{
								VolumeAttributes: map[string]string{
									"fsName": "myfs",
								},
							},
						},
					},
					Status: corev1.PersistentVolumeStatus{},
				}
				err := pvStore.Add(&pv)
				Expect(err).To(BeNil())

				// Verify CephFS PV is added to CephFSPVList
				Expect(pvStore.CephFSPVList).To(HaveKey(types.UID("cephfs-uid")))
				Expect(pvStore.CephFSPVList[types.UID("cephfs-uid")]).To(Equal("test-cephfs-pv"))

				// Verify FSName is extracted
				Expect(pvStore.FSName).To(Equal("myfs"))

				// Verify it's NOT in the RBD Store
				Expect(pvStore.Store).ToNot(HaveKey(types.UID("cephfs-uid")))
			})
		})
	})

	When("non PV object is added", func() {
		pvStore := NewPersistentVolumeStore(opts)
		It("should not add the object to cache", func() {
			err := pvStore.Add(corev1.PersistentVolumeClaim{})
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unexpected object of type"))
		})
	})

	When("PV update is detected", func() {
		pvStore := NewPersistentVolumeStore(opts)
		err := pvStore.Add(&corev1.PersistentVolume{})
		Expect(err).To(BeNil())
		It("should update PV in the cache", func() {
			err := pvStore.Update(&corev1.PersistentVolume{})
			Expect(err).To(BeNil())
		})
	})

	When("non PV update is detected", func() {
		pvStore := NewPersistentVolumeStore(opts)
		err := pvStore.Add(&corev1.PersistentVolume{})
		Expect(err).To(BeNil())
		It("should not update the cache", func() {
			err := pvStore.Update(nil)
			Expect(err).ToNot(BeNil())
		})
	})
})
