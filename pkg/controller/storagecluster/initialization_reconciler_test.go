package storagecluster

import (
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	openshiftv1 "github.com/openshift/api/template/v1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

var logt = logf.Log.WithName("controller_storageclusterinitialization_test")

func TestRecreatingStorageClusterInitialization(t *testing.T) {
	cr := &api.StorageClusterInitialization{}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createFakeInitializationStorageClusterReconciler(t, cr)
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
	reconciler := createFakeInitializationStorageClusterReconciler(t, cr)
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
	reconciler := createFakeInitializationStorageClusterReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.Equal(t, nil, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func createDefaultStorageCluster() *api.StorageCluster {
	return createStorageCluster("ocsinit", "zone", []string{"zone1", "zone2", "zone3"})
}

func createStorageCluster(scName, failureDomainName string,
	zoneTopologyLabels []string) *api.StorageCluster {
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: scName,
		},
		Status: api.StorageClusterStatus{
			FailureDomain: failureDomainName,
			NodeTopologies: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: zoneTopologyLabels,
				},
			},
		},
	}
	return cr
}

func createUpdateRuntimeObjects(cp *CloudPlatform) []runtime.Object {
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
	cbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	updateRTObjects := []runtime.Object{csfs, csrbd, cfs, cbp}

	if !isValidCloudPlatform(cp.platform) {
		csobc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-ceph-rgw",
			},
		}
		updateRTObjects = append(updateRTObjects, csobc)
	}

	// Create 'cephobjectstoreuser' only for non-aws platforms
	if !isValidCloudPlatform(cp.platform) {
		cosu := &cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstoreuser",
			},
		}
		updateRTObjects = append(updateRTObjects, cosu)
	}

	// Create 'cephobjectstore' only for non-cloud platforms
	if !isValidCloudPlatform(cp.platform) {
		cos := &cephv1.CephObjectStore{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstore",
			},
		}
		updateRTObjects = append(updateRTObjects, cos)
	}

	return updateRTObjects
}

func initStorageClusterResourceCreateUpdateTestWithPlatform(
	t *testing.T, platform *CloudPlatform, runtimeObjs []runtime.Object) (*testing.T, ReconcileStorageCluster, *api.StorageCluster, reconcile.Request) {
	cr := createDefaultStorageCluster()
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	rtObjsToCreateReconciler := []runtime.Object{&nbv1.NooBaa{}}
	// runtimeObjs are present, it means tests are for update
	// add all the update required changes
	if runtimeObjs != nil {
		tbd := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name: "rook-ceph-tools",
			},
		}
		rtObjsToCreateReconciler = append(rtObjsToCreateReconciler, tbd)
	}

	reconciler := createFakeInitializationStorageClusterReconcilerWithPlatform(
		t, platform, rtObjsToCreateReconciler...)

	_ = reconciler.client.Create(nil, cr)
	for _, rtObj := range runtimeObjs {
		_ = reconciler.client.Create(nil, rtObj)
	}

	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	return t, reconciler, cr, request
}

func createFakeInitializationStorageClusterReconciler(t *testing.T, obj ...runtime.Object) ReconcileStorageCluster {
	return createFakeInitializationStorageClusterReconcilerWithPlatform(
		t, &CloudPlatform{platform: PlatformUnknown}, obj...)
}

func createFakeInitializationStorageClusterReconcilerWithPlatform(t *testing.T,
	platform *CloudPlatform,
	obj ...runtime.Object) ReconcileStorageCluster {
	scheme := createFakeInitializationScheme(t, obj...)
	obj = append(obj, mockNodeList)
	client := fake.NewFakeClientWithScheme(scheme, obj...)
	if platform == nil {
		platform = &CloudPlatform{platform: PlatformUnknown}
	}

	return ReconcileStorageCluster{
		client:    client,
		scheme:    scheme,
		reqLogger: logf.Log.WithName("controller_storagecluster_test"),
		platform:  platform,
	}
}

func createFakeInitializationScheme(t *testing.T, obj ...runtime.Object) *runtime.Scheme {
	registerObjs := obj
	registerObjs = append(registerObjs)
	api.SchemeBuilder.Register(registerObjs...)
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}
	err = cephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add cephv1 scheme")
	}
	err = storagev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add storagev1 scheme")
	}
	err = openshiftv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add openshiftv1 scheme")
	}
	err = snapapi.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add volume-snapshot scheme")
	}

	return scheme
}
