package storageautoscaling

import (
	"context"
	"crypto/tls"
	"fmt"
	"net/http"
	"time"

	"github.com/go-logr/logr"
	prometheusapi "github.com/prometheus/client_golang/api"
	prometheusv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	prometheusconfig "github.com/prometheus/common/config"
	"github.com/prometheus/common/model"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageAutoscalerReconciler is the reconciler for StorageAutoscaler objects
// nolint:revive
type StorageAutoscalerReconciler struct {
	client.Client
	ctx               context.Context
	Log               logr.Logger
	Scheme            *runtime.Scheme
	OperatorNamespace string
	RequeueTime       time.Duration
}

// SetupWithManager sets up the reconciler with the manager
func (r *StorageAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	storageAutoscalerReconcilerController := ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Secret{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))
		// todo: change configmap type to storageautoscaler

	return storageAutoscalerReconcilerController.Complete(r)
}

// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=*,verbs=get

// Reconcile reconciles the StorageAutoscaler object
func (r *StorageAutoscalerReconciler) Reconcile(ctx context.Context, req reconcile.Request) (reconcile.Result, error) {
	r.ctx = ctx
	// scrape the metrics from the prometheus serve
	scrapeMetrics, err := r.scrapeMetrics()
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return reconcile.Result{}, err
	}
	r.Log.Info("scraped metrics", "metrics", scrapeMetrics)

	// todo implement the reconcile logic

	return reconcile.Result{RequeueAfter: r.RequeueTime}, nil
}

func (r *StorageAutoscalerReconciler) scrapeMetrics() (model.Value, error) {
	prometheusURL := "https://prometheus-k8s.openshift-monitoring.svc.cluster.local:9091"
	if isROSAHCP, err := platform.IsPlatformROSAHCP(); err != nil {
		r.Log.Error(err, "Failed to determine if ROSA HCP cluster")
		return nil, err
	} else if isROSAHCP {
		prometheusURL = fmt.Sprintf("https://prometheus.%s.svc.cluster.local:9339", r.OperatorNamespace)
	}

	caCertPath := "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt"
	tokenPath := "/var/run/secrets/kubernetes.io/serviceaccount/token"

	// create auth roundtripper
	tlsConfig, err := prometheusconfig.NewTLSConfig(&prometheusconfig.TLSConfig{
		CAFile: caCertPath,
	})
	if err != nil {
		r.Log.Error(err, "failed to create tls config")
		return nil, err
	}
	settings := prometheusconfig.TLSRoundTripperSettings{}
	newRT := func(cfg *tls.Config) (http.RoundTripper, error) {
		return &http.Transport{TLSClientConfig: cfg}, nil
	}
	tlsRoundTripper, err := prometheusconfig.NewTLSRoundTripperWithContext(r.ctx, tlsConfig, settings, newRT)
	if err != nil {
		r.Log.Error(err, "failed to create tls round tripper")
		return nil, err
	}
	roundTripper := prometheusconfig.NewAuthorizationCredentialsRoundTripper("Bearer", prometheusconfig.NewFileSecret(tokenPath), tlsRoundTripper)
	// create prometheus client
	prometheusClient, err := prometheusapi.NewClient(prometheusapi.Config{
		Address:      prometheusURL,
		RoundTripper: roundTripper,
	})
	if err != nil {
		r.Log.Error(err, "failed to create prometheus client")
		return nil, err
	}
	// scrape the metrics
	scraper := prometheusv1.NewAPI(prometheusClient)
	osdUsageQuery := "(ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes_used / ceph_osd_stat_bytes))"
	result, warnings, err := scraper.Query(r.ctx, osdUsageQuery, time.Now(), prometheusv1.WithTimeout(10*time.Second))
	if err != nil {
		r.Log.Error(err, "failed to query prometheus")
		return nil, err
	}

	if len(warnings) > 0 {
		r.Log.Info("prometheus query warnings", "warnings", warnings)
	}

	return result, nil
}
