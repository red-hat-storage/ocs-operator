package handler

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	metricsPath = "/metrics"
	healthzPath = "/healthz"
	readyzPath  = "/readyz"
)

type CustomResourceMuxConfig struct {
	CustomResourceRegistry *prometheus.Registry
	ExporterRegistry       *prometheus.Registry
	HandlerOpts            promhttp.HandlerOpts
	ReadyFn                func() bool
}

func RegisterExporterMuxHandlers(mux *http.ServeMux, exporterRegistry *prometheus.Registry, opts promhttp.HandlerOpts) {
	metricsHandler := promhttp.HandlerFor(exporterRegistry, opts)
	mux.Handle(metricsPath, metricsHandler)
}

func RegisterCustomResourceMuxHandlers(mux *http.ServeMux, cfg CustomResourceMuxConfig) {
	metricsHandler := InstrumentMetricHandler(cfg.ExporterRegistry,
		promhttp.HandlerFor(cfg.CustomResourceRegistry, cfg.HandlerOpts),
	)
	mux.Handle(metricsPath, metricsHandler)

	mux.HandleFunc(healthzPath, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	if cfg.ReadyFn != nil {
		mux.HandleFunc(readyzPath, func(w http.ResponseWriter, _ *http.Request) {
			if cfg.ReadyFn() {
				w.WriteHeader(http.StatusOK)
			} else {
				w.WriteHeader(http.StatusServiceUnavailable)
			}
		})
	}
}

// InstrumentMetricHandler is a middleware that wraps the provided http.Handler
// to observe requests sent to the exporter
func InstrumentMetricHandler(registry *prometheus.Registry, handler http.Handler) http.Handler {
	requestsTotal := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "ocs_exporter_requests_total",
		Help: "Total number of scrapes.",
	}, []string{"code"})

	requestsInFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "ocs_exporter_requests_in_flight",
		Help: "Current number of scrapes being served.",
	})

	requestDuration := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "ocs_exporter_request_duration_seconds",
		Help: "Duration of all scrapes.",
	}, []string{"code"})

	registry.MustRegister(
		requestsTotal,
		requestsInFlight,
		requestDuration,
	)

	return promhttp.InstrumentHandlerDuration(
		requestDuration,
		promhttp.InstrumentHandlerInFlight(requestsInFlight,
			promhttp.InstrumentHandlerCounter(requestsTotal, handler),
		),
	)
}
