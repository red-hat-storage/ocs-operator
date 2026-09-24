package storagecluster

import (
	"context"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestMetricsTLSSpecForCluster(t *testing.T) {
	sc := &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			ExternalStorage: ocsv1.ExternalStorageClusterSpec{Enable: true},
		},
	}
	spec := metricsTLSSpecForCluster(sc)
	if spec.SecretName != "" {
		t.Fatal("expected empty metrics TLS spec for external cluster")
	}

	sc = &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			EnableRookServicesTLS: false,
		},
	}
	spec = metricsTLSSpecForCluster(sc)
	if spec.SecretName != "" {
		t.Fatal("expected empty metrics TLS spec when toggle is disabled")
	}
}

func TestMetricsTLSSpecForClusterWhenEnabled(t *testing.T) {
	sc := &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			EnableRookServicesTLS: true,
		},
	}
	spec := metricsTLSSpecForCluster(sc)
	if spec.SecretName == "" {
		t.Fatal("expected metrics TLS spec when enabled")
	}
	if spec.SecretName != cephMgrMetricsTLSSecretName {
		t.Fatalf("unexpected secret name %q", spec.SecretName)
	}
	if spec.CA.ConfigMap == nil {
		t.Fatal("expected ConfigMap CA for OpenShift service CA")
	}
	if spec.CA.ConfigMap.Name != cephMgrMetricsTLSConfigMapName {
		t.Fatalf("unexpected CA ConfigMap name %q", spec.CA.ConfigMap.Name)
	}
	if spec.CA.ConfigMap.Key != openShiftServiceCASecretKey {
		t.Fatalf("unexpected CA ConfigMap key %q", spec.CA.ConfigMap.Key)
	}
}

func TestDeleteCephMgrMetricsTLSResources(t *testing.T) {
	const namespace = "openshift-storage"

	scheme := createFakeScheme(t)
	sc := &ocsv1.StorageCluster{
		TypeMeta: metav1.TypeMeta{APIVersion: ocsv1.GroupVersion.String(), Kind: "StorageCluster"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-storagecluster",
			Namespace: namespace,
		},
		Spec: ocsv1.StorageClusterSpec{
			Monitoring: &ocsv1.MonitoringSpec{},
		},
	}
	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cephMgrServiceName,
			Namespace: namespace,
			Annotations: map[string]string{
				rookCephv1.ServiceServingCertKey: cephMgrMetricsTLSSecretName,
			},
		},
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cephMgrMetricsTLSConfigMapName,
			Namespace: namespace,
		},
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cephMgrMetricsTLSSecretName,
			Namespace: namespace,
		},
	}

	reconciler := &StorageClusterReconciler{
		Client: fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(sc, svc, cm, secret).Build(),
		Scheme: scheme,
	}

	if err := reconciler.reconcileCephMetricsTLS(context.TODO(), sc); err != nil {
		t.Fatalf("reconcileCephMetricsTLS failed: %v", err)
	}

	err := reconciler.Get(context.TODO(), types.NamespacedName{Name: cephMgrMetricsTLSConfigMapName, Namespace: namespace}, &corev1.ConfigMap{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected ConfigMap to be deleted, got: %v", err)
	}

	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: cephMgrMetricsTLSSecretName, Namespace: namespace}, &corev1.Secret{})
	if !apierrors.IsNotFound(err) {
		t.Fatalf("expected Secret to be deleted, got: %v", err)
	}

	updatedSvc := &corev1.Service{}
	if err := reconciler.Get(context.TODO(), types.NamespacedName{Name: cephMgrServiceName, Namespace: namespace}, updatedSvc); err != nil {
		t.Fatalf("expected Service to remain: %v", err)
	}
	if _, ok := updatedSvc.Annotations[rookCephv1.ServiceServingCertKey]; ok {
		t.Fatal("expected serving-cert annotation to be removed from Service")
	}
}
