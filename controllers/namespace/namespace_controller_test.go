package namespace

import (
	"context"
	"strings"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func getReconciler(t *testing.T) NamespaceReconciler {
	ns := &corev1.Namespace{}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(ns).Build()
	log := logf.Log.WithName("controller_namespace_test")

	return NamespaceReconciler{
		Scheme: scheme,
		Client: client,
		Log:    log,
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	err := v1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}

	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}

	return scheme
}

func TestReconcilerImplementsInterface(t *testing.T) {
	reconciler := NamespaceReconciler{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestNamespaceAnnotated(t *testing.T) {
	ctx := context.TODO()
	storageTestNS := "openshift-storage-test"
	storageNS := "openshift-storage"
	aroLoggingNS := "openshift-azure-logging"
	arnNS := "openshift-azure"

	reconciler := getReconciler(t)

	namespace := []string{storageNS, storageTestNS, aroLoggingNS, arnNS}
	for _, name := range namespace {
		ns := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: name,
			},
		}
		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: name,
			},
		}
		err := reconciler.Create(ctx, ns)
		assert.NoError(t, err, "failed to create namespace")
		_, err = reconciler.Reconcile(ctx, request)
		assert.NoError(t, err, "failed to reconcile")
	}

	nsList := &corev1.NamespaceList{}
	err := reconciler.Client.List(ctx, nsList)
	assert.NoError(t, err, "failed to list namespaces")
	for _, testNs := range nsList.Items {
		if strings.Contains(testNs.Name, storageNS) || strings.Contains(testNs.Name, storageTestNS) {
			assert.Contains(t, testNs.Annotations["reclaimspace.csiaddons.openshift.io/schedule"], "@weekly")
		}
		// Ensure that no annotation set on these namespaces
		if strings.Contains(testNs.Name, arnNS) || strings.Contains(testNs.Name, aroLoggingNS) {
			assert.NotContains(t, testNs.Annotations, "reclaimspace.csiaddons.openshift.io/schedule")
		}
	}
}
