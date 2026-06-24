package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/go-logr/zapr"
	"github.com/oklog/run"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/collectors"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/exporter"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/handler"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/certwatcher"
	"sigs.k8s.io/controller-runtime/pkg/metrics/filters"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var _ promhttp.Logger = promhttplogger{}

type promhttplogger struct{}

func (log promhttplogger) Println(v ...interface{}) {
	klog.Errorln(v...)
}

func main() {
	opts := options.NewOptions()
	opts.AddFlags()
	opts.Parse()
	if opts.Help {
		opts.Usage()
		os.Exit(0)
	}

	zapLogger := zap.Must(zap.NewProduction())
	if opts.IsDevelopment {
		zapLogger = zap.Must(zap.NewDevelopment())
	}
	klog.SetLogger(zapr.NewLogger(zapLogger))
	defer klog.Flush()

	if len(opts.AllowedNamespaces) == 0 {
		klog.Fatal("at least one namespace must be specified via --namespaces")
	}

	klog.Infof("using options: %+v", opts)

	kubeconfig, err := clientcmd.BuildConfigFromFlags(opts.Apiserver, opts.KubeconfigPath)
	if err != nil {
		klog.Fatalf("failed to create cluster config: %v", err)
	}
	opts.Kubeconfig = kubeconfig

	// Create authentication filter if secure serving is enabled
	var authFilter metricsserver.Filter
	if opts.SecureServing {
		httpClient, err := rest.HTTPClientFor(kubeconfig)
		if err != nil {
			klog.Fatalf("failed to create http client: %v", err)
		}
		authFilter, err = filters.WithAuthenticationAndAuthorization(kubeconfig, httpClient)
		if err != nil {
			klog.Fatalf("failed to create auth filter: %v", err)
		}
		klog.Info("authentication and authorization enabled for metrics endpoints")
	}

	promHandlerOpts := func(registry *prometheus.Registry) promhttp.HandlerOpts {
		return promhttp.HandlerOpts{
			ErrorLog:      promhttplogger{},
			ErrorHandling: promhttp.ContinueOnError,
			Registry:      registry,
		}
	}

	exporterRegistry := prometheus.NewRegistry()
	exporter.RegisterExporterCollectors(exporterRegistry)

	exporterMux := http.NewServeMux()
	handler.RegisterExporterMuxHandlers(exporterMux, exporterRegistry, promHandlerOpts(exporterRegistry))

	// Separate connections for RBD and CephFS because go-ceph IOContext
	// operations are not safe to share across concurrent scan goroutines
	// that target different pools/namespaces.
	var rbdConn, cephfsConn *cephconn.Conn
	if !opts.NoCeph {
		var err error
		rbdConn, err = cephconn.NewConn(opts)
		if err != nil {
			klog.Fatalf("failed to initialize rbd ceph connection: %v", err)
		}
		defer rbdConn.Close()
		cephfsConn, err = cephconn.NewConn(opts)
		if err != nil {
			klog.Fatalf("failed to initialize cephfs ceph connection: %v", err)
		}
		defer cephfsConn.Close()
	}

	// StopCh and WaitForScans must run before connection Close (LIFO defer
	// order) so scan goroutines stop before their connections are torn down.
	opts.StopCh = make(chan struct{})
	defer func() {
		close(opts.StopCh)
		collectors.WaitForScans()
	}()

	// Start secret watcher for automatic credential rotation.
	// When the ocs-metrics-exporter-ceph-auth secret changes, trigger reconnection.
	if !opts.NoCeph {
		secretWatcher := cephconn.NewSecretWatcher(opts)
		secretWatcher.RegisterOnChange(rbdConn.Reconnect)
		secretWatcher.RegisterOnChange(cephfsConn.Reconnect)

		kubeClient, err := clientset.NewForConfig(opts.Kubeconfig)
		if err != nil {
			klog.Fatalf("failed to create kubernetes client for secret watcher: %v", err)
		}
		lw := cephconn.CreateSecretListWatch(kubeClient, opts.AllowedNamespaces[0], util.OcsMetricsExporterCephClientName)
		reflector := cache.NewReflector(lw, &corev1.Secret{}, secretWatcher, 5*time.Minute)
		go reflector.Run(opts.StopCh)
		klog.Info("started secret watcher for cephx key rotation")
	}

	customResourceRegistry := prometheus.NewRegistry()
	readyFn := func() bool { return true }
	if opts.NoCeph {
		collectors.RegisterNonCephCollectors(customResourceRegistry, opts)
	} else {
		collectors.RegisterCustomResourceCollectors(customResourceRegistry, opts)
		rbdCollector := collectors.RegisterCephRBDCollector(customResourceRegistry, rbdConn, opts)
		collectors.RegisterCephBlocklistCollector(customResourceRegistry, rbdCollector)
		collectors.RegisterCephFSMetricsCollector(customResourceRegistry, cephfsConn, opts)
		readyFn = collectors.CephReady
	}

	customResourceMux := http.NewServeMux()
	handler.RegisterCustomResourceMuxHandlers(customResourceMux, handler.CustomResourceMuxConfig{
		CustomResourceRegistry: customResourceRegistry,
		ExporterRegistry:       exporterRegistry,
		HandlerOpts:            promHandlerOpts(customResourceRegistry),
		ReadyFn:                readyFn,
	})

	// Wrap handlers with auth filter if secure serving is enabled
	// Health endpoints (/healthz, /readyz) are excluded from authentication
	// to allow kubelet probes to work without credentials
	var customResourceHandler, exporterHandler http.Handler = customResourceMux, exporterMux
	if authFilter != nil {
		log := zapr.NewLogger(zapLogger)
		authenticatedHandler, err := authFilter(log.WithValues("server", "metrics"), customResourceMux)
		if err != nil {
			klog.Fatalf("failed to apply auth filter to custom resource handler: %v", err)
		}
		// Wrap with health bypass - health endpoints go directly to mux, others go through auth
		customResourceHandler = healthBypassHandler(customResourceMux, authenticatedHandler)

		exporterHandler, err = authFilter(log.WithValues("server", "exporter"), exporterMux)
		if err != nil {
			klog.Fatalf("failed to apply auth filter to exporter handler: %v", err)
		}
	}

	// Set up signal handling for graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		klog.Infof("received signal %v, initiating shutdown", sig)
		cancel()
	}()

	var rg run.Group

	// Add servers to run group
	if opts.SecureServing {
		rg.Add(listenAndServeTLS(ctx, customResourceHandler, opts.Host, opts.Port, opts.TLSCertFile, opts.TLSKeyFile))
		rg.Add(listenAndServeTLS(ctx, exporterHandler, opts.ExporterHost, opts.ExporterPort, opts.TLSCertFile, opts.TLSKeyFile))
		klog.Infof("Running metrics server (HTTPS) on %s:%v", opts.Host, opts.Port)
		klog.Infof("Running telemetry server (HTTPS) on %s:%v", opts.ExporterHost, opts.ExporterPort)
	} else {
		rg.Add(listenAndServe(ctx, customResourceHandler, opts.Host, opts.Port))
		rg.Add(listenAndServe(ctx, exporterHandler, opts.ExporterHost, opts.ExporterPort))
		klog.Infof("Running metrics server (HTTP) on %s:%v", opts.Host, opts.Port)
		klog.Infof("Running telemetry server (HTTP) on %s:%v", opts.ExporterHost, opts.ExporterPort)
	}

	// Add signal handler to run group
	rg.Add(func() error {
		<-ctx.Done()
		return ctx.Err()
	}, func(error) {
		cancel()
	})

	if err = rg.Run(); err != nil && err != context.Canceled {
		klog.Fatalf("metrics and telemetry servers terminated: %v", err)
	}
	klog.Info("shutdown complete")
}

func listenAndServe(ctx context.Context, handler http.Handler, host string, port int) (func() error, func(error)) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	server := &http.Server{
		Addr:    addr,
		Handler: handler,
	}
	return serverRunFuncs(ctx, server, addr)
}

func listenAndServeTLS(ctx context.Context, handler http.Handler, host string, port int, certFile, keyFile string) (func() error, func(error)) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	// Create certificate watcher for automatic cert reload
	certWatcher, err := certwatcher.New(certFile, keyFile)
	if err != nil {
		return func() error {
			return fmt.Errorf("failed to create certificate watcher: %w", err)
		}, func(error) {}
	}

	tlsConfig := &tls.Config{
		GetCertificate: certWatcher.GetCertificate,
		MinVersion:     tls.VersionTLS12,
	}

	server := &http.Server{
		Addr:      addr,
		Handler:   handler,
		TLSConfig: tlsConfig,
	}

	serve := func() error {
		// Start certificate watcher in background
		go func() {
			if err := certWatcher.Start(ctx); err != nil {
				klog.Errorf("certificate watcher error: %v", err)
			}
		}()

		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		tlsListener := tls.NewListener(listener, tlsConfig)
		return server.Serve(tlsListener)
	}

	cleanup := func(error) {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("failed to shutdown server: %v", err)
		}
	}

	return serve, cleanup
}

func serverRunFuncs(ctx context.Context, server *http.Server, addr string) (func() error, func(error)) {
	serve := func() error {
		listener, err := net.Listen("tcp", addr)
		if err != nil {
			return err
		}
		return server.Serve(listener)
	}
	cleanup := func(error) {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			klog.Errorf("failed to shutdown server: %v", err)
		}
	}
	return serve, cleanup
}

// healthBypassHandler returns a handler that routes health check endpoints
// directly to the unauthenticated handler, while all other requests go through
// the authenticated handler. This allows kubelet probes to work without credentials.
func healthBypassHandler(unauthenticated, authenticated http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz", "/readyz":
			unauthenticated.ServeHTTP(w, r)
		default:
			authenticated.ServeHTTP(w, r)
		}
	})
}
