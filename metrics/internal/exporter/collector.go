package exporter

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/version"
)

// NewExporterVersionCollector registers a Gauge metric describing the exporter
// version.
func NewExporterVersionCollector() prometheus.Collector {
	exporterVersion := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ocs_exporter_version",
		Help: "Version of the exporter.",
		ConstLabels: map[string]string{
			"version": version.GetVersion(),
		},
	})
	exporterVersion.Set(1)

	return exporterVersion
}
