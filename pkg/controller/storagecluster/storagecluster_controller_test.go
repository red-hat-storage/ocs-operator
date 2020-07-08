package storagecluster

import (
	"fmt"
	"testing"

	"github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"

	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
)

const (
	zoneTopologyLabel = "failure-domain.kubernetes.io/zone"
	hostnameLabel     = "kubernetes.io/hostname"
)

var mockStorageClusterRequest = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Name:      "storage-test",
		Namespace: "storage-test-ns",
	},
}

var mockStorageCluster = &api.StorageCluster{
	TypeMeta: metav1.TypeMeta{
		Kind: "StorageCluster",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      mockStorageClusterRequest.Name,
		Namespace: mockStorageClusterRequest.Namespace,
	},

	Status: api.StorageClusterStatus{
		StorageClassesCreated:       true,
		CephObjectStoresCreated:     true,
		CephObjectStoreUsersCreated: true,
		CephBlockPoolsCreated:       true,
		CephFilesystemsCreated:      true,
	},
}

var mockCephCluster = &rookCephv1.CephCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      generateNameForCephCluster(mockStorageCluster),
		Namespace: mockStorageCluster.Namespace,
	},
}

var mockCephClusterNamespacedName = types.NamespacedName{
	Name:      generateNameForCephCluster(mockStorageCluster),
	Namespace: mockStorageCluster.Namespace,
}

var mockStorageClusterInit = &api.StorageClusterInitialization{
	TypeMeta: metav1.TypeMeta{
		Kind: "StorageClusterInitialization",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "storage-test",
		Namespace: "storage-test-ns",
	},
}

var storageClassName = "gp2"
var volMode = corev1.PersistentVolumeBlock
var mockDeviceSets = []api.StorageDeviceSet{
	{
		Name:  "mock-sds",
		Count: 3,
		DataPVCTemplate: corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Ti"),
					},
				},
				StorageClassName: &storageClassName,
				VolumeMode:       &volMode,
			},
		},
		MetadataPVCTemplate: &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
				Resources: corev1.ResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("1Ti"),
					},
				},
				StorageClassName: &storageClassName,
				VolumeMode:       &volMode,
			},
		},
		Portable: true,
	},
}

var mockNodeList = &corev1.NodeList{
	TypeMeta: metav1.TypeMeta{
		Kind: "NodeList",
	},
	Items: []corev1.Node{
		corev1.Node{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					hostnameLabel:            "node1",
					zoneTopologyLabel:        "zone1",
					defaults.NodeAffinityKey: "",
				},
			},
		},
		corev1.Node{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					hostnameLabel:            "node2",
					zoneTopologyLabel:        "zone2",
					defaults.NodeAffinityKey: "",
				},
			},
		},
		corev1.Node{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
				Labels: map[string]string{
					hostnameLabel:            "node3",
					zoneTopologyLabel:        "zone3",
					defaults.NodeAffinityKey: "",
				},
			},
		},
	},
}

func TestReconcilerImplInterface(t *testing.T) {
	reconciler := ReconcileStorageCluster{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestNonWatchedResourceNameNotFound(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "doesn't exist",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedResourceNamespaceNotFound(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "doesn't exist",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithNoCephClusterType(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, cr, nodeList)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithTheCephClusterType(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	cc.Status.State = rookCephv1.ClusterStateCreated

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, mockStorageClusterInit, cc, nodeList)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestNodeTopologyMapNoNodes(t *testing.T) {
	nodeList := &corev1.NodeList{}

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, nodeList)
	err := reconciler.reconcileNodeTopologyMap(mockStorageCluster, reconciler.reqLogger)
	assert.Equal(t, err, fmt.Errorf("Not enough nodes found: Expected %d, found %d", defaults.DeviceSetReplica, len(nodeList.Items)))
	assert.Equal(t, reconciler.nodeCount, 0)
}

func TestNodeTopologyMapPreexistingRack(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	sc.Status.FailureDomain = "rack"

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
			defaults.RackTopologyKey: []string{
				"rack0",
				"rack1",
				"rack2",
			},
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, sc, nodeList)
	err := reconciler.reconcileNodeTopologyMap(sc, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.Equal(t, reconciler.nodeCount, 3)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, nodeTopologyMap, actual.Status.NodeTopologies)
}

func TestNodeTopologyMapTwoAZ(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	nodeList.Items[2].Labels[zoneTopologyLabel] = "zone2"

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, sc, nodeList)
	err := reconciler.reconcileNodeTopologyMap(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	nodeTopologyMap.Add(defaults.RackTopologyKey, "rack0")
	nodeTopologyMap.Add(defaults.RackTopologyKey, "rack1")
	nodeTopologyMap.Add(defaults.RackTopologyKey, "rack2")

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, nodeTopologyMap, actual.Status.NodeTopologies)
}

func TestNodeTopologyMapThreeAZ(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, sc, nodeList)
	err := reconciler.reconcileNodeTopologyMap(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, nodeTopologyMap, actual.Status.NodeTopologies)
}

func TestFailureDomain(t *testing.T) {
	sc := &api.StorageCluster{}
	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{},
	}
	sc.Status.NodeTopologies = nodeTopologyMap

	failureDomain := determineFailureDomain(sc)
	assert.Equal(t, "rack", failureDomain)

	nodeTopologyMap.Labels[zoneTopologyLabel] = []string{
		"zone1",
		"zone2",
		"zone3",
	}

	failureDomain = determineFailureDomain(sc)
	assert.Equal(t, "zone", failureDomain)
}

func TestEnsureCephClusterCreate(t *testing.T) {
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	cc.ObjectMeta.Name = "doesn't exist"

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, cc)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(mockStorageCluster, "", 3, log)
	actual := newCephCluster(mockStorageCluster, "", 3, log)
	err = reconciler.client.Get(nil, mockCephClusterNamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterUpdate(t *testing.T) {
	reconciler := createFakeStorageClusterReconciler(t, mockCephCluster)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(mockStorageCluster, "", 3, log)
	actual := newCephCluster(mockStorageCluster, "", 3, log)
	err = reconciler.client.Get(nil, mockCephClusterNamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterNoConditions(t *testing.T) {
	cc := newCephCluster(mockStorageCluster, "", 3, log)
	cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key" //for test purpose
	reconciler := createFakeStorageClusterReconciler(t, cc)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.NotEmpty(t, reconciler.conditions)
	assert.Len(t, reconciler.conditions, 3)

	expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
		conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
		conditionsv1.ConditionProgressing: corev1.ConditionTrue,
		conditionsv1.ConditionUpgradeable: corev1.ConditionFalse,
	}
	for cType, status := range expectedConditions {
		found := assertCondition(reconciler.conditions, cType, status)
		assert.True(t, found, "expected status condition not found", cType, status)
	}
}

func TestEnsureCephClusterNegativeConditions(t *testing.T) {
	cc := newCephCluster(mockStorageCluster, "", 3, log)
	cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
	cc.Status.State = rookCephv1.ClusterStateCreated
	reconciler := createFakeStorageClusterReconciler(t, cc)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.conditions)
}

func TestStorageClusterCephClusterCreation(t *testing.T) {
	// if both monPVCTemplate and monDataDirHostPath is provided via storageCluster
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonDataDirHostPath = "/test/path"
	sc.Spec.MonPVCTemplate = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}}
	actual := newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, actual.Spec.Mon.VolumeClaimTemplate.GetName(), sc.Spec.MonPVCTemplate.GetName())
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)

	// if only monDataDirHostPath is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonDataDirHostPath = "/test/path"
	actual = newCephCluster(sc, "", 3, log)
	var emptyPVCSpec *corev1.PersistentVolumeClaim
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, emptyPVCSpec, actual.Spec.Mon.VolumeClaimTemplate)
	assert.Equal(t, "/test/path", actual.Spec.DataDirHostPath)

	// if only monPVCTemplate is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonPVCTemplate = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}}
	actual = newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, sc.Spec.MonPVCTemplate, actual.Spec.Mon.VolumeClaimTemplate)
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)

	// if no monPVCTemplate and no monDataDirHostPath is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	actual = newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	pvcSpec := actual.Spec.Mon.VolumeClaimTemplate.Spec
	assert.Equal(t, mockDeviceSets[0].DataPVCTemplate.Spec.StorageClassName, pvcSpec.StorageClassName)
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)
}

func TestStorageClassDeviceSetCreation(t *testing.T) {
	sc := &api.StorageCluster{}
	sc.Spec.StorageDeviceSets = mockDeviceSets

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}
	sc.Status.NodeTopologies = nodeTopologyMap

	actual := newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	deviceSet := sc.Spec.StorageDeviceSets[0]
	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		assert.Equal(t, getPlacement(sc, "osd"), scds.Placement)
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}

	nodeTopologyMap.Labels[zoneTopologyLabel] = append(nodeTopologyMap.Labels[zoneTopologyLabel], "zone3")

	actual = newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
		assert.Equal(t, zoneTopologyLabel, topologyKey)
		matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		assert.Equal(t, 2, len(matchExpressions))
		nodeSelector := matchExpressions[1]
		assert.Equal(t, zoneTopologyLabel, nodeSelector.Key)
		assert.Equal(t, nodeTopologyMap.Labels[zoneTopologyLabel][i], nodeSelector.Values[0])
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}

	// Test with an empty label selector present in the StorageCluster.
	// This used to trigger a segfault (nil pointer dereference) in
	// newStorageClassDeviceSets. Make sure we don't regress.
	var emptyLabelSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{},
	}
	sc.Spec.LabelSelector = &emptyLabelSelector

	actual = newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
		assert.Equal(t, zoneTopologyLabel, topologyKey)
		matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		assert.Equal(t, 1, len(matchExpressions))
		nodeSelector := matchExpressions[0]
		assert.Equal(t, zoneTopologyLabel, nodeSelector.Key)
		assert.Equal(t, nodeTopologyMap.Labels[zoneTopologyLabel][i], nodeSelector.Values[0])
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}
}

func TestStorageDeviceSets(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets

	storageClassEBS := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gp2",
		},
		Provisioner: string(EBS),
		Parameters: map[string]string{
			"type": "gp2",
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, storageClassEBS)
	err := reconciler.validateStorageDeviceSets(sc)
	assert.NoError(t, err)

	metadataScName := ""
	sc.Spec.StorageDeviceSets[0].MetadataPVCTemplate.Spec.StorageClassName = &metadataScName
	err = reconciler.validateStorageDeviceSets(sc)
	assert.Contains(t, err.Error(), "no StorageClass specified for metadataPVCTemplate")

	sc.Spec.StorageDeviceSets[0].MetadataPVCTemplate.Spec.StorageClassName = nil
	err = reconciler.validateStorageDeviceSets(sc)
	assert.Contains(t, err.Error(), "no StorageClass specified for metadataPVCTemplate")

	scName := ""
	sc.Spec.StorageDeviceSets[0].DataPVCTemplate.Spec.StorageClassName = &scName
	err = reconciler.validateStorageDeviceSets(sc)
	assert.Contains(t, err.Error(), "no StorageClass specified")

	sc.Spec.StorageDeviceSets[0].DataPVCTemplate.Spec.StorageClassName = nil
	err = reconciler.validateStorageDeviceSets(sc)
	assert.Contains(t, err.Error(), "no StorageClass specified")
}

func TestStorageClusterInitConditions(t *testing.T) {
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cc.Status.State = rookCephv1.ClusterStateCreated

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, mockStorageClusterInit, cc, nodeList)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestStorageClusterFinalizer(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
	}
	noobaaMock := &v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, noobaaMock, nodeList)

	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Ensure finalizer exists in the beginning
	sc := &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	noobaa := &v1alpha1.NooBaa{}
	err = reconciler.client.Get(nil, namespacedName, noobaa)
	assert.NoError(t, err)
	assert.Equal(t, noobaa.Name, noobaaMock.Name)

	// Issue a delete
	now := metav1.Now()
	sc.SetDeletionTimestamp(&now)
	err = reconciler.client.Update(nil, sc)
	assert.NoError(t, err)

	sc = &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	err = reconciler.client.Delete(nil, noobaa)
	assert.NoError(t, err)

	result, err = reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Finalizer is removed
	sc = &api.StorageCluster{}
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 0)

	noobaa = &v1alpha1.NooBaa{}
	err = reconciler.client.Get(nil, namespacedName, noobaa)
	assert.True(t, errors.IsNotFound(err))
}

func assertExpectedCondition(t *testing.T, conditions []conditionsv1.Condition) {
	expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
		api.ConditionReconcileComplete:    corev1.ConditionTrue,
		conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
		conditionsv1.ConditionProgressing: corev1.ConditionTrue,
		conditionsv1.ConditionDegraded:    corev1.ConditionFalse,
		conditionsv1.ConditionUpgradeable: corev1.ConditionUnknown,
	}
	for cType, status := range expectedConditions {
		found := assertCondition(conditions, cType, status)
		assert.True(t, found, "expected status condition not found", cType, status)
	}
}

func assertCondition(conditions []conditionsv1.Condition, conditionType conditionsv1.ConditionType, status corev1.ConditionStatus) bool {
	for _, objCondition := range conditions {
		if objCondition.Type == conditionType {
			if objCondition.Status == status {
				return true
			}
		}
	}
	return false
}

func createFakeStorageClusterReconciler(t *testing.T, obj ...runtime.Object) ReconcileStorageCluster {
	scheme := createFakeScheme(t)
	client := fake.NewFakeClientWithScheme(scheme, obj...)

	return ReconcileStorageCluster{
		client:    client,
		scheme:    scheme,
		reqLogger: logf.Log.WithName("controller_storagecluster_test"),
		platform:  &CloudPlatform{},
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}
	err = storagev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add storagev1 scheme")
	}
	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}
	err = openshiftv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add openshiftv1 scheme")
	}
	return scheme
}

func TestMonCountChange(t *testing.T) {
	for nodeCount := 0; nodeCount <= 10; nodeCount++ {
		monCountExpected := defaults.MonCountMin
		if nodeCount >= defaults.MonCountMax {
			monCountExpected = defaults.MonCountMax
		}
		monCountActual := getMonCount(nodeCount)
		assert.Equal(t, monCountExpected, monCountActual)

	}

}

func TestNodeTopologyMapLabelSelector(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.LabelSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			metav1.LabelSelectorRequirement{
				Key:      WorkerAffinityKey,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	for i := 0; i < len(nodeList.Items); i++ {
		_, ok := nodeList.Items[i].ObjectMeta.Labels[defaults.NodeAffinityKey]
		if ok {
			delete(nodeList.Items[i].ObjectMeta.Labels, defaults.NodeAffinityKey)
		}
		nodeList.Items[i].ObjectMeta.Labels[WorkerAffinityKey] = ""
	}
	reconciler := createFakeStorageClusterReconciler(t, sc, nodeList)
	reconciler.reconcileNodeTopologyMap(sc, reconciler.reqLogger)
	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}
	assert.Equal(t, nodeTopologyMap, sc.Status.NodeTopologies)
}
