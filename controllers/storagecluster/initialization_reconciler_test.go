package storagecluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/blang/semver/v4"
	oprverion "github.com/operator-framework/api/pkg/lib/version"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsversion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
			Finalizers:      []string{storageClusterFinalizer},
			OwnerReferences: []metav1.OwnerReference{{Name: "storage-test", Kind: "StorageSystem", APIVersion: "v1"}},
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

func createUpdateRuntimeObjects(t *testing.T) []client.Object {
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

	skip, err := platform.PlatformsShouldSkipObjectStore()
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

func initStorageClusterResourceCreateUpdateTestProviderMode(t *testing.T, runtimeObjs []client.Object,
	customSpec *api.StorageClusterSpec, storageProfiles *api.StorageProfileList, remoteConsumers bool) (*testing.T, StorageClusterReconciler,
	*api.StorageCluster, reconcile.Request, []reconcile.Request) {
	cr := createDefaultStorageCluster()
	if customSpec != nil {
		_ = mergo.Merge(&cr.Spec, customSpec)
	}
	requestOCSInit := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	requests := []reconcile.Request{requestOCSInit}

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

	if remoteConsumers {
		node := &v1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Labels: map[string]string{
					"node-role.kubernetes.io/worker": "",
				},
			},
			Status: v1.NodeStatus{
				Addresses: []v1.NodeAddress{
					{
						Type:    v1.NodeInternalIP,
						Address: "0:0:0:0",
					},
				},
			},
		}

		os.Setenv(providerAPIServerImage, "fake-image")
		os.Setenv(util.WatchNamespaceEnvVar, "")
		os.Setenv(onboardingValidationKeysGeneratorImage, "fake-image")

		deployment := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		deployment.Status.AvailableReplicas = 1

		service := &v1.Service{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}
		service.Status.LoadBalancer.Ingress = []v1.LoadBalancerIngress{
			{
				Hostname: "fake",
			},
		}

		secret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		}

		clientConfigMap := &v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: ocsClientConfigMapName},
		}

		addedRuntimeObjects := []runtime.Object{node, service, deployment, secret, clientConfigMap}
		rtObjsToCreateReconciler = append(rtObjsToCreateReconciler, addedRuntimeObjects...)

	}

	// Unpacks StorageProfile list to runtime objects array
	var storageProfileRuntimeObjects []runtime.Object
	for _, sp := range storageProfiles.Items {
		storageProfileRuntimeObjects = append(storageProfileRuntimeObjects, &sp)
	}
	rtObjsToCreateReconciler = append(rtObjsToCreateReconciler, storageProfileRuntimeObjects...)

	reconciler := createFakeInitializationStorageClusterReconciler(t, rtObjsToCreateReconciler...)

	_ = reconciler.Client.Create(context.TODO(), cr)
	for _, rtObj := range runtimeObjs {
		_ = reconciler.Client.Create(context.TODO(), rtObj)
	}

	var requestsStorageProfiles []reconcile.Request
	for _, sp := range storageProfiles.Items {
		requestSP := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      sp.Name,
				Namespace: cr.Namespace,
			},
		}
		_ = reconciler.Client.Create(context.TODO(), &sp)
		requestsStorageProfiles = append(requestsStorageProfiles, requestSP)
		requests = append(requests, requestSP)

	}

	spList := &api.StorageProfileList{}
	_ = reconciler.Client.List(context.TODO(), spList)
	for _, request := range requests {
		err := os.Setenv("OPERATOR_NAMESPACE", request.Namespace)
		assert.NoError(t, err)
		result, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		err = os.Setenv("WATCH_NAMESPACE", request.Namespace)
		assert.NoError(t, err)
	}

	return t, reconciler, cr, requestOCSInit, requestsStorageProfiles
}

func initStorageClusterResourceCreateUpdateTest(t *testing.T, runtimeObjs []client.Object,
	customSpec *api.StorageClusterSpec) (*testing.T, StorageClusterReconciler,
	*api.StorageCluster, reconcile.Request) {
	cr := createDefaultStorageCluster()
	if customSpec != nil {
		_ = mergo.Merge(&cr.Spec, customSpec)
	}
	requestOCSInit := reconcile.Request{
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

	reconciler := createFakeInitializationStorageClusterReconciler(
		t, rtObjsToCreateReconciler...)

	_ = reconciler.Client.Create(context.TODO(), cr)
	for _, rtObj := range runtimeObjs {
		_ = reconciler.Client.Create(context.TODO(), rtObj)
	}

	err := os.Setenv("OPERATOR_NAMESPACE", cr.Namespace)
	assert.NoError(t, err)
	result, err := reconciler.Reconcile(context.TODO(), requestOCSInit)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	err = os.Setenv("WATCH_NAMESPACE", cr.Namespace)
	assert.NoError(t, err)

	return t, reconciler, cr, requestOCSInit
}

func createFakeInitializationStorageClusterReconciler(t *testing.T, obj ...runtime.Object) StorageClusterReconciler {
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
	verOcs, err := semver.Make(ocsversion.Version)
	if err != nil {
		panic(err)
	}
	csv := &opv1a1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("ocs-operator-%s", sc.Name),
			Namespace: sc.Namespace,
		},
		Spec: opv1a1.ClusterServiceVersionSpec{
			Version: oprverion.OperatorVersion{Version: verOcs},
		},
	}

	rookCephMonSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rook-ceph-mon", Namespace: sc.Namespace},
		Data: map[string][]byte{
			"fsid": []byte(cephFSID),
		},
	}

	statusSubresourceObjs := []client.Object{sc}
	var runtimeObjects []runtime.Object

	for _, objIt := range obj {
		if objIt != nil {
			if objIt.GetObjectKind().GroupVersionKind().Kind == "StorageProfile" {
				statusSubresourceObjs = append(statusSubresourceObjs, objIt.(client.Object))
			} else {
				runtimeObjects = append(runtimeObjects, objIt)
			}
		}
	}

	runtimeObjects = append(runtimeObjects, mockNodeList.DeepCopy(), cbp, cfs, cnfs, cnfsbp, cnfssvc, infrastructure, networkConfig, rookCephMonSecret, csv)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(runtimeObjects...).WithStatusSubresource(statusSubresourceObjs...).Build()

	return StorageClusterReconciler{
		Client:            client,
		Scheme:            scheme,
		OperatorCondition: newStubOperatorCondition(),
		Log:               logf.Log.WithName("controller_storagecluster_test"),
	}
}
