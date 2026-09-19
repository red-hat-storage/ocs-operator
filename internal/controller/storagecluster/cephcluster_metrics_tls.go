package storagecluster

import (
	"context"
	"fmt"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	cephMgrServiceName             = "rook-ceph-mgr"
	cephMgrMetricsTLSConfigMapName = "rook-ceph-mgr-metrics-service-ca"
	cephMgrMetricsHTTPSPortName   = "https-metrics"
	cephMgrMetricsTLSSecretName   = "rook-ceph-prometheus-server-tls"
	openShiftInjectCABundleAnnotation = "service.beta.openshift.io/inject-cabundle"
	openShiftServiceCASecretKey    = "service-ca.crt"
)

func (r *StorageClusterReconciler) reconcileCephMetricsTLS(ctx context.Context, sc *ocsv1.StorageCluster) error {
	if sc.Spec.Monitoring == nil || sc.Spec.ExternalStorage.Enable {
		return nil
	}

	if err := r.ensureCephClusterMetricsTLSSpec(ctx, sc); err != nil {
		return err
	}

	if !sc.Spec.Monitoring.EnableCephMetricsTLS {
		return nil
	}

	if err := r.ensureCephMgrMetricsTLSConfigMap(ctx, sc); err != nil {
		return err
	}
	if err := r.ensureCephMgrMetricsServiceServingCert(ctx, sc); err != nil {
		return err
	}
	return r.ensureCephMgrMetricsServiceMonitorTLS(ctx, sc)
}

func (r *StorageClusterReconciler) ensureCephClusterMetricsTLSSpec(ctx context.Context, sc *ocsv1.StorageCluster) error {
	cephCluster := &rookCephv1.CephCluster{}
	err := r.Get(ctx, types.NamespacedName{Name: util.GenerateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	enableTLS := sc.Spec.Monitoring != nil && sc.Spec.Monitoring.EnableCephMetricsTLS

	var patch []byte
	if enableTLS {
		patch = []byte(fmt.Sprintf(`{"spec":{"monitoring":{"metricsTLS":{"enabled":true,"secretName":%q}}}}`, cephMgrMetricsTLSSecretName))
	} else {
		patch = []byte(`{"spec":{"monitoring":{"metricsTLS":null}}}`)
	}

	return r.Patch(ctx, cephCluster, client.RawPatch(types.MergePatchType, patch))
}

func (r *StorageClusterReconciler) ensureCephMgrMetricsTLSConfigMap(ctx context.Context, sc *ocsv1.StorageCluster) error {
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cephMgrMetricsTLSConfigMapName,
			Namespace: sc.Namespace,
			Annotations: map[string]string{
				openShiftInjectCABundleAnnotation: "true",
			},
		},
	}
	if err := controllerutil.SetControllerReference(sc, cm, r.Scheme); err != nil {
		return err
	}

	existing := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cm.Name, Namespace: cm.Namespace}, existing)
	if apierrors.IsNotFound(err) {
		return r.Create(ctx, cm)
	} else if err != nil {
		return err
	}

	if existing.Annotations != nil && existing.Annotations[openShiftInjectCABundleAnnotation] == "true" {
		return nil
	}

	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}

	existing.Annotations[openShiftInjectCABundleAnnotation] = "true"
	return r.Update(ctx, existing)
}

func (r *StorageClusterReconciler) ensureCephMgrMetricsServiceServingCert(ctx context.Context, sc *ocsv1.StorageCluster) error {
	svc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: cephMgrServiceName, Namespace: sc.Namespace}, svc)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if svc.Annotations != nil && svc.Annotations[rookCephv1.ServiceServingCertKey] == cephMgrMetricsTLSSecretName {
		return nil
	}
	if svc.Annotations == nil {
		svc.Annotations = map[string]string{}
	}
	svc.Annotations[rookCephv1.ServiceServingCertKey] = cephMgrMetricsTLSSecretName
	return r.Update(ctx, svc)
}

func (r *StorageClusterReconciler) ensureCephMgrMetricsServiceMonitorTLS(ctx context.Context, sc *ocsv1.StorageCluster) error {
	sm := &monitoringv1.ServiceMonitor{}
	err := r.Get(ctx, types.NamespacedName{Name: cephMgrServiceName, Namespace: sc.Namespace}, sm)
	if apierrors.IsNotFound(err) {
		return nil
	} else if err != nil {
		return err
	}

	if len(sm.Spec.Endpoints) == 0 {
		return nil
	}

	desiredCA := monitoringv1.SecretOrConfigMap{
		ConfigMap: &corev1.ConfigMapKeySelector{
			LocalObjectReference: corev1.LocalObjectReference{Name: cephMgrMetricsTLSConfigMapName},
			Key:                  openShiftServiceCASecretKey,
		},
	}

	endpoint := &sm.Spec.Endpoints[0]
	if endpoint.Port != cephMgrMetricsHTTPSPortName {
		return nil
	}
	if endpoint.TLSConfig != nil && equality.Semantic.DeepEqual(endpoint.TLSConfig.CA, desiredCA) {
		return nil
	}
	if endpoint.TLSConfig == nil {
		endpoint.TLSConfig = &monitoringv1.TLSConfig{}
	}

	endpoint.TLSConfig.CA = desiredCA
	return r.Update(ctx, sm)
}
