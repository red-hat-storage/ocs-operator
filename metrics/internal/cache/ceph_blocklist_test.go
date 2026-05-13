package cache

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"k8s.io/client-go/rest"
)

var _ = Describe("CephBlocklistStore", func() {
	var opts *options.Options

	BeforeEach(func() {
		opts = &options.Options{
			Kubeconfig:        &rest.Config{},
			AllowedNamespaces: []string{"openshift-storage"},
			CephAuthNamespace: "openshift-storage",
		}
	})

	Describe("NewCephBlocklistStore", func() {
		It("should create a new CephBlocklistStore", func() {
			store := NewCephBlocklistStore(opts)
			Expect(store).ToNot(BeNil())
			Expect(store.Store).To(BeEmpty())
			Expect(store.cephClusterNamespace).To(Equal("openshift-storage"))
			Expect(store.cephAuthNamespace).To(Equal("openshift-storage"))
		})
	})

	Describe("InvalidateCredentials", func() {
		It("should clear the monitorConfig", func() {
			store := NewCephBlocklistStore(opts)
			// Set up some credentials
			store.monitorConfig = cephMonitorConfig{
				monitor: "test-monitor",
				id:      "test-id",
				key:     "test-key",
			}
			Expect(store.monitorConfig).ToNot(Equal(cephMonitorConfig{}))

			// Invalidate credentials
			store.InvalidateCredentials()

			// Verify credentials were cleared
			Expect(store.monitorConfig).To(Equal(cephMonitorConfig{}))
		})
	})
})
