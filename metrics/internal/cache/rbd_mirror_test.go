package cache

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var _ = Describe("RBDMirror Cache", func() {
	defer GinkgoRecover()
	opts := &options.Options{
		Kubeconfig:        &rest.Config{},
		AllowedNamespaces: []string{""},
		CephAuthNamespace: "",
	}

	When("new cache is requested", func() {
		It("should return a new empty RBDMirror cache", func() {
			rbdMirrorStore := NewRBDMirrorStore(opts)
			Expect(rbdMirrorStore).ToNot(BeNil())
			Expect(rbdMirrorStore.Store).To(BeEmpty())
		})
	})

	When("new CephBlockPool is added", func() {
		rbdMirrorStore := NewRBDMirrorStore(opts)
		rbdMirrorStore.initCephFn = func(kubeclient clientset.Interface, cephClusterNamespace, cephAuthNamespace string) (cephMonitorConfig, error) {
			return cephMonitorConfig{}, nil
		}
		rbdMirrorStore.rbdImageStatusFn = func(config *cephMonitorConfig, poolName string) (RBDMirrorStatusVerbose, error) {
			return RBDMirrorStatusVerbose{}, nil
		}
		rbdMirrorStore.cephClusterNamespace = "test-namespace"
		rbdMirrorStore.cephAuthNamespace = "test-namespace"

		When("CephBlockPool mirroring is not enabled", func() {
			pool := cephv1.CephBlockPool{
				ObjectMeta: v1.ObjectMeta{
					Name:      "cephblock-without-mirroring",
					Namespace: "test-namespace",
					UID:       types.UID("cephblock-without-mirroring"),
				},
				Spec: cephv1.NamedBlockPoolSpec{
					PoolSpec: cephv1.PoolSpec{
						Mirroring: cephv1.MirroringSpec{
							Enabled: false,
						},
					},
				},
			}
			It("should not return an error", func() {
				err := rbdMirrorStore.Add(&pool)
				Expect(err).To(BeNil())
			})
			It("should not add CephBlockPool to cache", func() {
				_, exists := rbdMirrorStore.Store[pool.GetUID()]
				Expect(exists).To(BeFalse())
			})
		})

		When("CephBlockPool mirroring is enabled", func() {
			pool := cephv1.CephBlockPool{
				ObjectMeta: v1.ObjectMeta{
					Name:      "cephblock-without-mirroring",
					Namespace: "test-namespace",
					UID:       types.UID("cephblock-without-mirroring"),
				},
				Spec: cephv1.NamedBlockPoolSpec{
					PoolSpec: cephv1.PoolSpec{
						Mirroring: cephv1.MirroringSpec{
							Enabled: true,
						},
					},
				},
			}
			It("should not return an error", func() {
				err := rbdMirrorStore.Add(&pool)
				Expect(err).To(BeNil())
			})
			It("should add CephBlockPool to cache", func() {
				_, exists := rbdMirrorStore.Store[pool.GetUID()]
				Expect(exists).To(BeTrue())
			})
		})
	})

	When("new unexpected object is added", func() {
		rbdMirrorStore := NewRBDMirrorStore(opts)
		unexpectedObject := cephv1.CephCluster{
			ObjectMeta: v1.ObjectMeta{
				Name:      "unexpected-cephcluster-object",
				Namespace: "test-namespace",
			},
		}
		It("should not be added to cache", func() {
			err := rbdMirrorStore.Add(&unexpectedObject)
			Expect(err).ToNot(BeNil())
			Expect(err.Error()).To(ContainSubstring("unexpected object of type"))
		})
	})

})
