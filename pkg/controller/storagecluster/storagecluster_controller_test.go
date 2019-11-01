package storagecluster

import (
	"fmt"
	"testing"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
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
		Name:      "storage-test",
		Namespace: "storage-test-ns",
	},

	Status: api.StorageClusterStatus{
		StorageClassesCreated:       true,
		CephObjectStoresCreated:     true,
		CephObjectStoreUsersCreated: true,
		CephBlockPoolsCreated:       true,
		CephFilesystemsCreated:      true,
	},
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
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, cr)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithTheCephClusterType(t *testing.T) {
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cephMock.Status.State = rookCephv1.ClusterStateCreated
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, mockStorageClusterInit, cephMock)
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
	var nodeTopologyMap *api.NodeTopologyMap

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, nodeList)
	err := reconciler.reconcileNodeTopologyMap(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)

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
	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{},
	}

	failureDomain := determineFailureDomain(nodeTopologyMap)
	assert.Equal(t, "rack", failureDomain)

	nodeTopologyMap.Labels[zoneTopologyLabel] = []string{
		"zone1",
		"zone2",
		"zone3",
	}

	failureDomain = determineFailureDomain(nodeTopologyMap)
	assert.Equal(t, "zone", failureDomain)
}

func TestEnsureCephClusterCreate(t *testing.T) {
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "doesn't exist",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, cephMock)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(mockStorageCluster, "")
	actual := newCephCluster(mockStorageCluster, "")
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterUpdate(t *testing.T) {
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cephMock)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(mockStorageCluster, "")
	actual := newCephCluster(mockStorageCluster, "")
	err = reconciler.client.Get(nil, mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterNoConditions(t *testing.T) {
	cc := newCephCluster(mockStorageCluster, "")
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
	cc := newCephCluster(mockStorageCluster, "")
	cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
	cc.Status.State = rookCephv1.ClusterStateCreated
	reconciler := createFakeStorageClusterReconciler(t, cc)
	err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.conditions)
}

func TestStorageClusterCephClusterCreation(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets

	actual := newCephCluster(sc, "")
	assert.Equal(t, sc.Name, actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	pvcSpec := actual.Spec.Mon.VolumeClaimTemplate.Spec
	assert.Equal(t, mockDeviceSets[0].DataPVCTemplate.Spec.StorageClassName, pvcSpec.StorageClassName)
}

func TestStorageClassDeviceSetCreation(t *testing.T) {
	deviceSet := mockDeviceSets[0]

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}

	actual := newStorageClassDeviceSets(mockDeviceSets, nodeTopologyMap)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		assert.Equal(t, defaults.DaemonPlacements["osd"], scds.Placement)
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, false, scds.Portable)
	}

	nodeTopologyMap.Labels[zoneTopologyLabel] = append(nodeTopologyMap.Labels[zoneTopologyLabel], "zone3")

	actual = newStorageClassDeviceSets(mockDeviceSets, nodeTopologyMap)
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
}

func TestStorageClusterInitConditions(t *testing.T) {
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cephMock.Status.State = rookCephv1.ClusterStateCreated
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, mockStorageClusterInit, cephMock)
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
	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}
	return scheme
}
