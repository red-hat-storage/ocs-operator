package storagecluster

import (
	"context"
	"os"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	openshiftv1 "github.com/openshift/api/template/v1"
	api "github.com/openshift/ocs-operator/api/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func createDefaultStorageCluster() *api.StorageCluster {
	return createStorageCluster("ocsinit", "zone", []string{"zone1", "zone2", "zone3"})
}

func createStorageCluster(scName, failureDomainName string,
	zoneTopologyLabels []string) *api.StorageCluster {
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: scName,
		},
		Spec: api.StorageClusterSpec{
			Monitoring: &api.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyIgnore),
			},
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

func createUpdateRuntimeObjects(t *testing.T, cp *Platform, r StorageClusterReconciler) []client.Object {
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
	updateRTObjects := []client.Object{csfs, csrbd, cfs, cbp}

	avoid, err := r.PlatformsShouldAvoidObjectStore()
	assert.NoError(t, err)
	if !avoid {
		csobc := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-ceph-rgw",
			},
		}
		updateRTObjects = append(updateRTObjects, csobc)

		// Create 'cephobjectstoreuser' only for non-aws platforms
		cosu := &cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstoreuser",
			},
		}
		updateRTObjects = append(updateRTObjects, cosu)

		// Create 'cephobjectstore' only for non-cloud platforms
		cos := &cephv1.CephObjectStore{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstore",
			},
		}
		updateRTObjects = append(updateRTObjects, cos)

		// Create 'route' only for non-cloud platforms
		cosr := &routev1.Route{
			ObjectMeta: metav1.ObjectMeta{
				Name: "ocsinit-cephobjectstore",
			},
		}
		updateRTObjects = append(updateRTObjects, cosr)
	}
	return updateRTObjects
}

func initStorageClusterResourceCreateUpdateTestWithPlatform(
	t *testing.T, platform *Platform, runtimeObjs []client.Object) (*testing.T, StorageClusterReconciler, *api.StorageCluster, reconcile.Request) {
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

	_ = reconciler.Client.Create(context.TODO(), cr)
	for _, rtObj := range runtimeObjs {
		_ = reconciler.Client.Create(context.TODO(), rtObj)
	}

	result, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	err = os.Setenv("WATCH_NAMESPACE", cr.Namespace)
	assert.NoError(t, err)

	return t, reconciler, cr, request
}

func createFakeInitializationStorageClusterReconciler(t *testing.T, obj ...runtime.Object) StorageClusterReconciler {
	return createFakeInitializationStorageClusterReconcilerWithPlatform(
		t, &Platform{platform: configv1.NonePlatformType}, obj...)
}

func createFakeInitializationStorageClusterReconcilerWithPlatform(t *testing.T,
	platform *Platform,
	obj ...runtime.Object) StorageClusterReconciler {
	scheme := createFakeInitializationScheme(t, obj...)
	obj = append(obj, mockNodeList)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).Build()
	if platform == nil {
		platform = &Platform{platform: configv1.NonePlatformType}
	}

	return StorageClusterReconciler{
		Client:        client,
		Scheme:        scheme,
		serverVersion: &version.Info{},
		Log:           logf.Log.WithName("controller_storagecluster_test"),
		platform:      platform,
	}
}

func createFakeInitializationScheme(t *testing.T, obj ...runtime.Object) *runtime.Scheme {
	registerObjs := obj
	registerObjs = append(registerObjs) //nolint:staticcheck
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
	err = monitoringv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add monitoringv1 scheme")
	}
	err = extv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add extv1 scheme")
	}
	err = routev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add routev1 scheme")
	}

	err = batchv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add batchv1 scheme")
	}

	return scheme
}
