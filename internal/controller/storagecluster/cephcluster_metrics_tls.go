package storagecluster

import (
	"context"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	cephMgrServiceName                = "rook-ceph-mgr"
	cephMgrMetricsTLSConfigMapName    = "rook-ceph-mgr-metrics-service-ca"
	cephMgrMetricsTLSSecretName       = "rook-ceph-prometheus-server-tls"
	openShiftInjectCABundleAnnotation = "service.beta.openshift.io/inject-cabundle"
	openShiftServiceCASecretKey       = "service-ca.crt"
)

func isMetricsTLSEnabled(sc *ocsv1.StorageCluster) bool {
	return !sc.Spec.ExternalStorage.Enable && sc.Spec.EnableRookServicesTLS
}

func metricsTLSSpecForCluster(sc *ocsv1.StorageCluster) rookCephv1.MetricsTLSSpec {
	if !isMetricsTLSEnabled(sc) {
		return rookCephv1.MetricsTLSSpec{}
	}
	return rookCephv1.MetricsTLSSpec{
		SecretName: cephMgrMetricsTLSSecretName,
		CA: rookCephv1.MetricsTLSCASpec{
			ConfigMap: &corev1.ConfigMapKeySelector{
				LocalObjectReference: corev1.LocalObjectReference{Name: cephMgrMetricsTLSConfigMapName},
				Key:                  openShiftServiceCASecretKey,
			},
		},
	}
}

func (r *StorageClusterReconciler) reconcileCephMetricsTLS(ctx context.Context, sc *ocsv1.StorageCluster) error {
	if !isMetricsTLSEnabled(sc) {
		return r.deleteCephMgrMetricsTLSResources(ctx, sc)
	}

	if err := r.ensureCephMgrMetricsTLSConfigMap(ctx, sc); err != nil {
		return err
	}
	return r.reconcileCephMgrServiceServingCertAnnotation(ctx, sc, true)
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

// reconcileCephMgrServiceServingCertAnnotation sets or removes the serving-cert
// annotation on the rook-ceph-mgr Service depending on the enable flag.
func (r *StorageClusterReconciler) reconcileCephMgrServiceServingCertAnnotation(ctx context.Context, sc *ocsv1.StorageCluster, enable bool) error {
	svc := &corev1.Service{}
	err := r.Get(ctx, types.NamespacedName{Name: cephMgrServiceName, Namespace: sc.Namespace}, svc)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}

	if enable {
		if svc.Annotations != nil && svc.Annotations[rookCephv1.ServiceServingCertKey] == cephMgrMetricsTLSSecretName {
			return nil
		}
		if svc.Annotations == nil {
			svc.Annotations = map[string]string{}
		}
		svc.Annotations[rookCephv1.ServiceServingCertKey] = cephMgrMetricsTLSSecretName
	} else {
		delete(svc.Annotations, rookCephv1.ServiceServingCertKey)
	}
	return r.Update(ctx, svc)
}

func (r *StorageClusterReconciler) deleteCephMgrMetricsTLSResources(ctx context.Context, sc *ocsv1.StorageCluster) error {
	if err := r.reconcileCephMgrServiceServingCertAnnotation(ctx, sc, false); err != nil {
		return err
	}
	if err := r.deleteCephMgrMetricsTLSSecret(ctx, sc); err != nil {
		return err
	}
	return r.deleteCephMgrMetricsTLSConfigMap(ctx, sc)
}

func (r *StorageClusterReconciler) deleteCephMgrMetricsTLSConfigMap(ctx context.Context, sc *ocsv1.StorageCluster) error {
	cm := &corev1.ConfigMap{}
	err := r.Get(ctx, types.NamespacedName{Name: cephMgrMetricsTLSConfigMapName, Namespace: sc.Namespace}, cm)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.Delete(ctx, cm)
}

func (r *StorageClusterReconciler) deleteCephMgrMetricsTLSSecret(ctx context.Context, sc *ocsv1.StorageCluster) error {
	secret := &corev1.Secret{}
	err := r.Get(ctx, types.NamespacedName{Name: cephMgrMetricsTLSSecretName, Namespace: sc.Namespace}, secret)
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return r.Delete(ctx, secret)
}
