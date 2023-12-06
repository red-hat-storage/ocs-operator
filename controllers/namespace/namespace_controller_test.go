package namespace

import (
	"context"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
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
	reconciler := getReconciler(t)
	opNS := &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "openshift-storage-test",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "openshift-storage-test",
			Namespace: "",
		},
	}

	err := reconciler.Create(ctx, opNS)
	assert.NoError(t, err, "failed to create namespace")
	_, err = reconciler.Reconcile(ctx, request)
	assert.NoError(t, err, "failed to reconcile")
	nsList := &corev1.NamespaceList{}
	err = reconciler.Client.List(ctx, nsList)
	assert.NoError(t, err, "failed to list namespaces")
	for _, testNs := range nsList.Items {
		assert.Equal(t, testNs.Name, "openshift-storage-test")
		assert.Equal(t, testNs.Annotations["reclaimspace.csiaddons.openshift.io/schedule"], "@weekly")
	}
}
