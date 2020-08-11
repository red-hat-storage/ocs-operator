package handler

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsPath = "/metrics"
)

// RegisterExporterMuxHandlers registers the handlers needed to serve the
// exporter self metrics
func RegisterExporterMuxHandlers(mux *http.ServeMux, exporterRegistry *prometheus.Registry) {
	metricsHandler := promhttp.HandlerFor(exporterRegistry, promhttp.HandlerOpts{})
	mux.Handle(metricsPath, metricsHandler)
}
