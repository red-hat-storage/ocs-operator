package main

import (
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/collectors"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/exporter"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/handler"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
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

	opts.StopCh = make(chan struct{})
	defer close(opts.StopCh)

	kubeconfig, err := clientcmd.BuildConfigFromFlags(opts.Apiserver, opts.KubeconfigPath)
	if err != nil {
		klog.Fatalf("failed to create cluster config: %v", err)
	}
	opts.Kubeconfig = kubeconfig

	exporterRegistry := prometheus.NewRegistry()
	// Add exporter self metrics collectors to the registry.
	exporter.RegisterExporterCollectors(exporterRegistry)

	// serves exporter self metrics
	exporterMux := http.NewServeMux()
	handler.RegisterExporterMuxHandlers(exporterMux, exporterRegistry)

	customResourceRegistry := prometheus.NewRegistry()
	// Add custom resource collectors to the registry.
	collectors.RegisterCustomResourceCollectors(customResourceRegistry, opts)

	// Add persistent volume attributes collector to the registry.
	collectors.RegisterPersistentVolumeAttributesCollector(customResourceRegistry, opts)

	// Add blocklist collector to the registry
	collectors.RegisterCephBlocklistCollector(customResourceRegistry, opts)

	// serves custom resources metrics
	customResourceMux := http.NewServeMux()
	handler.RegisterCustomResourceMuxHandlers(customResourceMux, customResourceRegistry, exporterRegistry)

	rbdRegistry := prometheus.NewRegistry()
	// Add rbd mirror metrics collector to registry
	collectors.RegisterRBDMirrorCollector(rbdRegistry, opts)

	// server rbd mirror metrics
	handler.RegisterRBDMirrorMuxHandlers(customResourceMux, rbdRegistry)

	var rg run.Group
	rg.Add(listenAndServe(exporterMux, opts.ExporterHost, opts.ExporterPort))
	rg.Add(listenAndServe(customResourceMux, opts.Host, opts.Port))

	klog.Infof("Running metrics server on %s:%v", opts.Host, opts.Port)
	klog.Infof("Running telemetry server on %s:%v", opts.ExporterHost, opts.ExporterPort)
	err = rg.Run()
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
