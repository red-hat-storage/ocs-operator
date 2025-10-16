package storagecluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	ocsversion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	oprverion "github.com/operator-framework/api/pkg/lib/version"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	createVirtualMachineCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "virtualmachines"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "kubevirt.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "kubevirt.io",
				Scope: extv1.NamespaceScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "VirtualMachine",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1",
						Served: true,
					},
				},
			},
		}
	}
	createStorageClientCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "storageclients"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "ocs.openshift.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "ocs.openshift.io",
				Scope: extv1.ClusterScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "StorageClient",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1alpha1",
						Served: true,
					},
				},
			},
		}
	}
	createVolumeGroupSnapshotClassCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "volumegroupsnapshotclasses"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "groupsnapshot.storage.k8s.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "groupsnapshot.storage.k8s.io",
				Scope: extv1.ClusterScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "VolumeGroupSnapshotClass",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1beta1",
						Served: true,
					},
				},
			},
		}
	}
	createOdfVolumeGroupSnapshotClassCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "volumegroupsnapshotclasses"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "groupsnapshot.storage.openshift.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "groupsnapshot.storage.openshift.io",
				Scope: extv1.ClusterScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "VolumeGroupSnapshotClass",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1beta1",
						Served: true,
					},
				},
			},
		}
	}
	createRookCephOperatorCSV = func(namespace string) *opv1a1.ClusterServiceVersion {
		return &opv1a1.ClusterServiceVersion{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rook-ceph-operator-csv",
				Namespace: namespace,
				Labels: map[string]string{
					fmt.Sprintf("operators.coreos.com/rook-ceph-operator.%s", namespace): "",
				},
			},
			Spec: opv1a1.ClusterServiceVersionSpec{
				InstallStrategy: opv1a1.NamedInstallStrategy{
					StrategySpec: opv1a1.StrategyDetailsDeployment{
						DeploymentSpecs: []opv1a1.StrategyDeploymentSpec{
							{
								Name: "rook-ceph-operator",
							},
						},
					},
				},
			},
			Status: opv1a1.ClusterServiceVersionStatus{
				Phase: opv1a1.CSVPhaseSucceeded,
			},
		}
	}
)

func createDefaultStorageCluster() *api.StorageCluster {
	return createStorageCluster("ocsinit", "zone", "topology.kubernetes.io/zone", []string{"zone1", "zone2", "zone3"})
}

func createStorageCluster(scName, failureDomainName string, failureDomainKey string,
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
			FailureDomain:    failureDomainName,
			FailureDomainKey: failureDomainKey,
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

func initStorageClusterResourceCreateUpdateTest(t *testing.T, runtimeObjs []client.Object,
	customSpec *api.StorageClusterSpec) (*testing.T, *StorageClusterReconciler,
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

func createFakeInitializationStorageClusterReconciler(t *testing.T, obj ...runtime.Object) *StorageClusterReconciler {
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
		Status: &cephv1.NFSStatus{
			Status: cephv1.Status{
				Phase: util.PhaseReady,
			},
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

	workerNode := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: "workerNode",
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
	os.Setenv(onboardingValidationKeysGeneratorImage, "fake-image")

	ocsProviderServiceDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		Status: appsv1.DeploymentStatus{
			AvailableReplicas: 1,
		},
	}

	ocsProviderService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		Status: v1.ServiceStatus{
			LoadBalancer: v1.LoadBalancerStatus{
				Ingress: []v1.LoadBalancerIngress{{Hostname: "fake"}},
			},
		},
	}

	ocsProviderServiceSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
	}

	operatorNamespace := os.Getenv("OPERATOR_NAMESPACE")
	verOcs, err := semver.Make(ocsversion.Version)
	if err != nil {
		panic(err)
	}
	csv := &opv1a1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("ocs-operator-%s", sc.Name),
			Namespace: operatorNamespace,
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

	clientConfigMap := &v1.ConfigMap{}
	clientConfigMap.Name = ocsClientConfigMapName
	clientConfigMap.Namespace = sc.Namespace

	consumer := &ocsv1a1.StorageConsumer{}
	consumer.Name = defaults.LocalStorageConsumerName
	consumer.UID = "fake-uid"

	obj = append(
		obj,
		mockNodeList.DeepCopy(),
		consumer,
		cbp,
		cfs,
		clientConfigMap,
		cnfs,
		cnfsbp,
		cnfssvc,
		infrastructure,
		networkConfig,
		rookCephMonSecret,
		csv,
		workerNode,
		ocsProviderServiceSecret,
		ocsProviderServiceDeployment,
		ocsProviderService,
		createVirtualMachineCRD(),
		createStorageClientCRD(),
		createVolumeGroupSnapshotClassCRD(),
		createOdfVolumeGroupSnapshotClassCRD(),
		createRookCephOperatorCSV(sc.Namespace),
	)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).WithStatusSubresource(sc).Build()

	r := &StorageClusterReconciler{
		Client:            client,
		Scheme:            scheme,
		OperatorCondition: newStubOperatorCondition(),
		Log:               logf.Log.WithName("controller_storagecluster_test"),
	}
	r.crdsBeingWatched.Store(VolumeGroupSnapshotClassCrdName, true)
	r.crdsBeingWatched.Store(OdfVolumeGroupSnapshotClassCrdName, true)
	return r
}
