package storagecluster

import (
	"context"
	"os"
	"testing"

	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
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
			Annotations: map[string]string{
				UninstallModeAnnotation: string(UninstallModeGraceful),
				CleanupPolicyAnnotation: string(CleanupPolicyDelete),
			},
			Finalizers: []string{storageClusterFinalizer},
		},
		Spec: api.StorageClusterSpec{
			Monitoring: &api.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyIgnore),
			},
			NFS: &api.NFSSpec{
				Enable: true,
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
	cnfs := &cephv1.CephNFS{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs",
		},
	}
	cnfsbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs-builtin-pool",
		},
	}
	cnfssvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs-service",
		},
	}
	cbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	crm := &cephv1.CephRBDMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephrbdmirror",
		},
	}
	updateRTObjects := []client.Object{csfs, csrbd, cfs, cbp, crm, cnfs, cnfsbp, cnfssvc}

	skip, err := r.PlatformsShouldSkipObjectStore()
	assert.NoError(t, err)
	if !skip {
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
	t *testing.T, platform *Platform, runtimeObjs []client.Object, customSpec *api.StorageClusterSpec) (*testing.T, StorageClusterReconciler, *api.StorageCluster, reconcile.Request) {
	cr := createDefaultStorageCluster()
	if customSpec != nil {
		_ = mergo.Merge(&cr.Spec, customSpec)
	}
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

	err := os.Setenv("OPERATOR_NAMESPACE", cr.Namespace)
	assert.NoError(t, err)
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
	sc := &api.StorageCluster{}
	scheme := createFakeScheme(t)
	cfs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
		Status: &cephv1.CephFilesystemStatus{
			Phase: cephv1.ConditionType(util.PhaseReady),
		},
	}

	infrastructure := &configv1.Infrastructure{
		ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
	}

	cnfs := &cephv1.CephNFS{
		ObjectMeta: metav1.ObjectMeta{Name: "ocsinit-cephnfs"},
		Status: &cephv1.Status{
			Phase: util.PhaseReady,
		},
	}

	cnfsbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{Name: "ocsinit-cephnfs-builtin-pool"},
		Status: &cephv1.CephBlockPoolStatus{
			Phase: cephv1.ConditionType(util.PhaseReady),
		},
	}

	cnfssvc := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs-service",
		},
	}

	cbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
		Status: &cephv1.CephBlockPoolStatus{
			Phase: cephv1.ConditionType(util.PhaseReady),
		},
	}

	obj = append(obj, mockNodeList.DeepCopy(), cbp, cfs, cnfs, cnfsbp, cnfssvc, infrastructure, networkConfig)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).WithStatusSubresource(sc).Build()
	if platform == nil {
		platform = &Platform{platform: configv1.NonePlatformType}
	}

	return StorageClusterReconciler{
		Client:            client,
		Scheme:            scheme,
		serverVersion:     &version.Info{},
		OperatorCondition: newStubOperatorCondition(),
		Log:               logf.Log.WithName("controller_storagecluster_test"),
		platform:          platform,
	}
}
