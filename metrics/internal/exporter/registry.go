package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
)

// RegisterExporterCollectors registers the following collectors in the given
// prometheus.Registry:
//   - prometheus.NewProcessCollector
//   - prometheus.NewGoCollector
//   - collectors.NewExporterVersionCollector
// This is intended to be used to expose metrics about the exporter.
func RegisterExporterCollectors(registry *prometheus.Registry) {
	registry.MustRegister(
		// Add the standard process and Go metrics to the registry.
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
		collectors.NewGoCollector(),
		// Add exporter version collector to the registry.
		NewExporterVersionCollector(),
	)
}
