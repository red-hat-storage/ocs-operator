package collectors

import (
	"github.com/openshift/ocs-operator/metrics/internal/options"
	"github.com/prometheus/client_golang/prometheus"
)

const (
	// name of the project/exporter
	namespace = "ocs"
)

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(registry *prometheus.Registry, opts *options.Options) {
	cephObjectStoreCollector := NewCephObjectStoreCollector(opts)
	cephBlockPoolCollector := NewCephBlockPoolCollector(opts)
	cephObjectStoreCollector.Run(opts.StopCh)
	cephBlockPoolCollector.Run(opts.StopCh)
	registry.MustRegister(
		cephObjectStoreCollector,
		cephBlockPoolCollector,
	)
}
