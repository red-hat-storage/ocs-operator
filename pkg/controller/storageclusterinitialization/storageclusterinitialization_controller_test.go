package storageclusterinitialization

import (
	"testing"

	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var logt = logf.Log.WithName("controller_storageclusterinitialization_test")

func TestReconcilerImplInterface(t *testing.T) {
	reconciler := ReconcileStorageClusterInitialization{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestRecreatingStorageClusterInitialization(t *testing.T) {
	cr := &api.StorageClusterInitialization{}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createFakeStorageClusterInitReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestStorageClusterInitializationWithUnExpectedNamespace(t *testing.T) {
	cr := &api.StorageClusterInitialization{}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit-test-not-found",
			Namespace: "ocsinit-test-not-found",
		},
	}
	reconciler := createFakeStorageClusterInitReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestInitStorageClusterWithOutResources(t *testing.T) {
	cr := &api.StorageClusterInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createFakeStorageClusterInitReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.Equal(t, nil, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestInitStorageClusterResourcesCreation(t *testing.T) {
	cr := &api.StorageClusterInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	csfs := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-sc-not-found",
		},
	}
	csrbd := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-scbd-not-found",
		},
	}
	cfs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cfs-not-found",
		},
	}
	cosu := &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cosu-not-found",
		},
	}
	cbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cbp-not-found",
		},
	}
	cos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cos-not-found",
		},
	}
	reconciler := createFakeStorageClusterInitReconciler(t, cr, cfs, cosu, cbp, cos, csfs, csrbd)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestInitStorageClusterResourcesUpdate(t *testing.T) {
	cr := &api.StorageClusterInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	csfs := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfs",
		},
	}
	csrbd := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-ceph-rbd",
		},
	}
	cfs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
	}
	cosu := &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstoreuser",
		},
	}
	cbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	cos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstore",
		},
	}
	tbd := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name: "rook-ceph-tools",
		},
	}
	reconciler := createFakeStorageClusterInitReconciler(t, cr, cfs, cosu, cbp, cos, csfs, csrbd, tbd)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	assertExpectedResources(t, reconciler, cr, request)
}

func assertExpectedResources(t assert.TestingT, reconciler ReconcileStorageClusterInitialization, cr *api.StorageClusterInitialization, request reconcile.Request) {
	actualSc1 := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfs",
		},
	}
	actualSc2 := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-ceph-rbd",
		},
	}
	request.Name = "ocsinit-cephfs"
	err := reconciler.client.Get(nil, request.NamespacedName, actualSc1)
	assert.NoError(t, err)

	request.Name = "ocsinit-ceph-rbd"
	err = reconciler.client.Get(nil, request.NamespacedName, actualSc2)
	assert.NoError(t, err)

	expected, err := reconciler.newStorageClasses(cr)
	assert.NoError(t, err)

	assert.Equal(t, expected[0].ObjectMeta, actualSc1.ObjectMeta)
	assert.Equal(t, expected[0].Provisioner, actualSc1.Provisioner)
	assert.Equal(t, expected[0].ReclaimPolicy, actualSc1.ReclaimPolicy)
	assert.Equal(t, expected[0].Parameters, actualSc1.Parameters)

	assert.Equal(t, expected[1].ObjectMeta, actualSc2.ObjectMeta)
	assert.Equal(t, expected[1].Provisioner, actualSc2.Provisioner)
	assert.Equal(t, expected[1].ReclaimPolicy, actualSc2.ReclaimPolicy)
	assert.Equal(t, expected[1].Parameters, actualSc2.Parameters)

	//
	actualFs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
	}
	request.Name = "ocsinit-cephfilesystem"
	err = reconciler.client.Get(nil, request.NamespacedName, actualFs)
	assert.NoError(t, err)

	expectedAf, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, expectedAf[0].ObjectMeta, actualFs.ObjectMeta)
	assert.Equal(t, expectedAf[0].Spec, actualFs.Spec)

	//
	actualCosu := &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstoreuser",
		},
	}
	request.Name = "ocsinit-cephobjectstoreuser"
	err = reconciler.client.Get(nil, request.NamespacedName, actualCosu)
	assert.NoError(t, err)

	expectedCosu, err := reconciler.newCephObjectStoreUserInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, expectedCosu[0].ObjectMeta, actualCosu.ObjectMeta)
	assert.Equal(t, expectedCosu[0].Spec, actualCosu.Spec)

	//
	actualCbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	request.Name = "ocsinit-cephblockpool"
	err = reconciler.client.Get(nil, request.NamespacedName, actualCbp)
	assert.NoError(t, err)

	expectedCbp, err := reconciler.newCephBlockPoolInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, expectedCbp[0].ObjectMeta, actualCbp.ObjectMeta)
	assert.Equal(t, expectedCbp[0].Spec, actualCbp.Spec)

	//
	actualCos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstore",
		},
	}
	request.Name = "ocsinit-cephobjectstore"
	err = reconciler.client.Get(nil, request.NamespacedName, actualCos)
	assert.NoError(t, err)

	expectedCos, err := reconciler.newCephObjectStoreInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, expectedCos[0].ObjectMeta, actualCos.ObjectMeta)
	assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
}

func createFakeStorageClusterInitReconciler(t *testing.T, obj ...runtime.Object) ReconcileStorageClusterInitialization {
	scheme := createFakeScheme(t, obj...)
	client := fake.NewFakeClientWithScheme(scheme, obj...)

	return ReconcileStorageClusterInitialization{
		client: client,
		scheme: scheme,
	}
}

func createFakeScheme(t *testing.T, obj ...runtime.Object) *runtime.Scheme {
	registerObjs := obj
	registerObjs = append(registerObjs)
	api.SchemeBuilder.Register(registerObjs...)
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	return scheme
}
