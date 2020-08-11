package main

import (
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/oklog/run"
	"github.com/openshift/ocs-operator/metrics/internal/options"
	"k8s.io/klog"
)

func main() {
	opts := options.NewOptions()
	opts.AddFlags()
	// parses the flags and ExitOnError so errors can be ignored
	opts.Parse()
	if opts.Help {
		// prints usage messages for flags
		opts.Usage()
		os.Exit(0)
	}
	klog.Infof("using options: %+v", opts)

	// serves exporter self metrics
	exporterMux := http.NewServeMux()
	// serves custom resources metrics
	customResourceMux := http.NewServeMux()

	var rg run.Group
	rg.Add(listenAndServe(exporterMux, opts.ExporterHost, opts.ExporterPort))
	rg.Add(listenAndServe(customResourceMux, opts.Host, opts.Port))

	klog.Infof("Running metrics server on %s:%v", opts.Host, opts.Port)
	klog.Infof("Running telemetry server on %s:%v", opts.ExporterHost, opts.ExporterPort)
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
