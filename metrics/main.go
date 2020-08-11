package main

import (
	"net"
	"net/http"
	"strconv"

	"github.com/oklog/run"
	"k8s.io/klog"
)

func main() {
	host := "0.0.0.0"
	customResourceMetricsPort := 8080
	exporterMetricsPort := 8081

	// serves exporter self metrics
	exporterMux := http.NewServeMux()
	// serves custom resources metrics
	customResourceMux := http.NewServeMux()

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
