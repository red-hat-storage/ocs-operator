package mirroring

import (
	"context"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	storageclusterctrl "github.com/red-hat-storage/ocs-operator/v4/internal/controller/storagecluster"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func TestReconcileRbdMirror_UsesGetPlacement(t *testing.T) {
	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := rookCephv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := ocsv1alpha1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	if err := ocsv1.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}

	namespace := "test-namespace"
	storageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-storagecluster",
			Namespace: namespace,
		},
	}

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-configmap",
			Namespace: namespace,
		},
	}

	fakeClient := fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(configMap, storageCluster).
		WithIndex(&ocsv1alpha1.StorageConsumer{}, util.AnnotationIndexName, func(obj client.Object) []string {
			consumer := obj.(*ocsv1alpha1.StorageConsumer)
			if val, ok := consumer.Annotations[util.RequestMaintenanceModeAnnotation]; ok {
				return []string{val}
			}
			return []string{}
		}).
		Build()

	reconciler := &MirroringReconciler{
		Client: fakeClient,
		Scheme: scheme,
		ctx:    context.TODO(),
		log:    logf.Log.WithName("controller_mirroring_test"),
	}

	errorOccurred := reconciler.reconcileRbdMirror(configMap, true)
	assert.False(t, errorOccurred)

	rbdMirror := &rookCephv1.CephRBDMirror{}
	err := fakeClient.Get(context.TODO(), types.NamespacedName{
		Name:      util.CephRBDMirrorName,
		Namespace: namespace,
	}, rbdMirror)
	assert.NoError(t, err)

	expectedPlacement := storageclusterctrl.GetPlacement(storageCluster, "rbd-mirror")
	assert.Equal(t, expectedPlacement, rbdMirror.Spec.Placement,
		"RBDMirror placement should match GetPlacement(storageCluster, 'rbd-mirror')")
}
