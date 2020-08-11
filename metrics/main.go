package main

import (
	"net"
	"net/http"
	"strconv"

	"github.com/oklog/run"
	"github.com/openshift/ocs-operator/metrics/internal/exporter"
	"github.com/openshift/ocs-operator/metrics/internal/handler"
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/klog"
)

func main() {
	host := "0.0.0.0"
	customResourceMetricsPort := 8080
	exporterMetricsPort := 8081

	exporterRegistry := prometheus.NewRegistry()
	// Add exporter self metrics collectors to the registry.
	exporter.RegisterExporterCollectors(exporterRegistry)

	// serves exporter self metrics
	exporterMux := http.NewServeMux()
	handler.RegisterExporterMuxHandlers(exporterMux, exporterRegistry)

	customResourceRegistry := prometheus.NewRegistry()
	// serves custom resources metrics
	customResourceMux := http.NewServeMux()
	handler.RegisterCustomResourceMuxHandlers(customResourceMux, customResourceRegistry, exporterRegistry)

	var rg run.Group
	rg.Add(listenAndServe(exporterMux, host, exporterMetricsPort))
	rg.Add(listenAndServe(customResourceMux, host, customResourceMetricsPort))

	klog.Infof("Running metrics server on %s:%v", "0.0.0.0", 8080)
	klog.Infof("Running telemetry server on %s:%v", "0.0.0.0", 8081)
	err := rg.Run()
	if err != nil {
		klog.Fatalf("metrics and telemetry servers terminated: %v", err)
	}
}

func listenAndServe(mux *http.ServeMux, host string, port int) (func() error, func(error)) {
	var listener net.Listener
	serve := func() error {
		addr := net.JoinHostPort(host, strconv.Itoa(port))
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return http.Serve(listener, mux)
	}
	cleanup := func(error) {
		err := listener.Close()
		if err != nil {
			klog.Errorf("failed to close listener: %v", err)
		}
	}
	return serve, cleanup
}
