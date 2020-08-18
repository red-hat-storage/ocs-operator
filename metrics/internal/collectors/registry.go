package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/client-go/rest"
)

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(kubeconfig *rest.Config, registry *prometheus.Registry, stopCh <-chan struct{}) {
	cephObjectStoreCollector := NewCephObjectStoreCollector(kubeconfig)
	cephObjectStoreCollector.Run(stopCh)
	registry.MustRegister(
		cephObjectStoreCollector,
	)
}
