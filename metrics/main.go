package main

import (
	"net"
	"net/http"
	"os"
	"strconv"

	"github.com/go-logr/zapr"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/collectors"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/exporter"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/handler"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"go.uber.org/zap"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

var _ promhttp.Logger = promhttplogger{}

type promhttplogger struct{}

func (log promhttplogger) Println(v ...interface{}) {
	klog.Errorln(v...)
}

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

	logr := zap.Must(zap.NewProduction())
	if opts.IsDevelopment {
		logr = zap.Must(zap.NewDevelopment())
	}
	klog.SetLogger(zapr.NewLogger(logr))
	defer klog.Flush()

	klog.Infof("using options: %+v", opts)
	opts.StopCh = make(chan struct{})
	defer close(opts.StopCh)

	kubeconfig, err := clientcmd.BuildConfigFromFlags(opts.Apiserver, opts.KubeconfigPath)
	if err != nil {
		klog.Fatalf("failed to create cluster config: %v", err)
	}
	opts.Kubeconfig = kubeconfig

	promHandlerOpts := func(registry *prometheus.Registry) promhttp.HandlerOpts {
		return promhttp.HandlerOpts{
			ErrorLog:      promhttplogger{},
			ErrorHandling: promhttp.ContinueOnError,
			Registry:      registry,
		}
	}

	exporterRegistry := prometheus.NewRegistry()
	// Add exporter self metrics collectors to the registry.
	exporter.RegisterExporterCollectors(exporterRegistry)

	// serves exporter self metrics
	exporterMux := http.NewServeMux()
	handler.RegisterExporterMuxHandlers(exporterMux, exporterRegistry, promHandlerOpts(exporterRegistry))

	customResourceRegistry := prometheus.NewRegistry()
	// Add custom resource collectors to the registry.
	collectors.RegisterCustomResourceCollectors(customResourceRegistry, opts)

	// Add persistent volume attributes collector to the registry.
	collectors.RegisterPersistentVolumeAttributesCollector(customResourceRegistry, opts)

	// Add blocklist collector to the registry
	collectors.RegisterCephBlocklistCollector(customResourceRegistry, opts)

	// Add rbd children collector to the registry
	collectors.RegisterCephRBDChildrenCollector(customResourceRegistry, opts)

	// Add CephFS subvolume count collector to the registry
	collectors.RegisterCephFSMetricsCollector(customResourceRegistry, opts)

	// serves custom resources metrics
	customResourceMux := http.NewServeMux()
	handler.RegisterCustomResourceMuxHandlers(customResourceMux, customResourceRegistry, exporterRegistry, promHandlerOpts(customResourceRegistry))

	rbdRegistry := prometheus.NewRegistry()
	// Add rbd mirror metrics collector to registry
	collectors.RegisterRBDMirrorCollector(rbdRegistry, opts)

	// server rbd mirror metrics
	handler.RegisterRBDMirrorMuxHandlers(customResourceMux, rbdRegistry, promHandlerOpts(rbdRegistry))

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
