package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(registry *prometheus.Registry) {
	registry.MustRegister()
}
