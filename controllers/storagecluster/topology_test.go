package storagecluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	api "github.com/openshift/ocs-operator/api/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
)

var rack0 = "rack0"
var rack1 = "rack1"

var workerAffinityNode = corev1.Node{
	ObjectMeta: metav1.ObjectMeta{
		Name: "workAffinityNode",
		Labels: map[string]string{
			WorkerAffinityKey: "",
		},
	},
}

var defaultAffinityNode = corev1.Node{
	ObjectMeta: metav1.ObjectMeta{
		Name: "defaultAffinityNode",
		Labels: map[string]string{
			defaults.NodeAffinityKey: "",
		},
	},
}

func TestReconcileNodeTopologyMap(t *testing.T) {
	testcases := []struct {
		label                   string
		nodeList                *corev1.NodeList
		storageCluster          *api.StorageCluster
		failureDomain           string
		useDefaultNodeList      bool
		expectedNodeTopologyMap *api.NodeTopologyMap
		expectedNodeCount       int
	}{
		{
			label:              "Case 1", // failure domain is rack
			nodeList:           &corev1.NodeList{},
			storageCluster:     &api.StorageCluster{},
			failureDomain:      "rack",
			useDefaultNodeList: true,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
					defaults.RackTopologyKey: []string{
						"rack0",
						"rack1",
						"rack2",
					},
				},
			},
			expectedNodeCount: 3,
		},
		{
			label:              "Case 2", // failure domain is not set and sufficient zones available
			nodeList:           &corev1.NodeList{},
			storageCluster:     &api.StorageCluster{},
			failureDomain:      "",
			useDefaultNodeList: true,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
				},
			},
			expectedNodeCount: 3,
		},
		{
			label: "Case 3", // failure domain is not set and insufficient zones available
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{Kind: "NodeList"},
				Items: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node1",
							Labels: map[string]string{
								zoneTopologyLabel:        "zone1",
								hostnameLabel:            "node1",
								defaults.NodeAffinityKey: "",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node2",
							Labels: map[string]string{
								zoneTopologyLabel:        "zone2",
								hostnameLabel:            "node2",
								defaults.NodeAffinityKey: "",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node3",
							Labels: map[string]string{
								hostnameLabel:            "node3",
								defaults.NodeAffinityKey: "",
							},
						},
					},
				},
			},
			storageCluster:     &api.StorageCluster{},
			failureDomain:      "",
			useDefaultNodeList: false,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
					defaults.RackTopologyKey: []string{
						"rack0",
						"rack1",
						"rack2",
					},
				},
			},
			expectedNodeCount: 3,
		},
		{
			label: "Case 4", // failure domain is not set and insufficient zones and regions available
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{Kind: "NodeList"},
				Items: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node1",
							Labels: map[string]string{
								zoneTopologyLabel:        "zone1",
								hostnameLabel:            "node1",
								defaults.NodeAffinityKey: "",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node2",
							Labels: map[string]string{
								zoneTopologyLabel:        "zone2",
								hostnameLabel:            "node2",
								defaults.NodeAffinityKey: "",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node3",
							Labels: map[string]string{
								regionTopologyLabel:      "region3",
								hostnameLabel:            "node3",
								defaults.NodeAffinityKey: "",
							},
						},
					},
				},
			},
			storageCluster:     &api.StorageCluster{},
			failureDomain:      "",
			useDefaultNodeList: false,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
					},
					regionTopologyLabel: []string{
						"region3",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
					defaults.RackTopologyKey: []string{
						"rack0",
						"rack1",
						"rack2",
					},
				},
			},
			expectedNodeCount: 3,
		},
	}

	for _, tc := range testcases {
		mockStorageCluster.DeepCopyInto(tc.storageCluster)
		if tc.useDefaultNodeList == true {
			mockNodeList.DeepCopyInto(tc.nodeList)
		}
		tc.storageCluster.Status.FailureDomain = tc.failureDomain
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster, tc.nodeList)
		err := reconciler.reconcileNodeTopologyMap(tc.storageCluster)
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedNodeCount, reconciler.nodeCount, "[%s]: failed to get correct node count", tc.label)

		err = reconciler.Client.Status().Update(context.TODO(), tc.storageCluster)
		assert.NoError(t, err)

		actual := &api.StorageCluster{}
		err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedNodeTopologyMap, actual.Status.NodeTopologies, "[%s]: failed to get correct nodeToplogies", tc.label)
	}
}

func TestNodeTopologyMapOnDifferentAZ(t *testing.T) {
	testcases := []struct {
		label                   string
		nodeList                *corev1.NodeList
		storageCluster          *api.StorageCluster
		zoneCount               int
		expectedNodeTopologyMap *api.NodeTopologyMap
	}{
		{
			label:          "Case 1", // three nodes spread across a single zone
			nodeList:       &corev1.NodeList{},
			storageCluster: &api.StorageCluster{},
			zoneCount:      1,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
					defaults.RackTopologyKey: []string{
						"rack0",
						"rack1",
						"rack2",
					},
				},
			},
		},
		{
			label:          "Case 2", // three nodes spread across two zones
			nodeList:       &corev1.NodeList{},
			storageCluster: &api.StorageCluster{},
			zoneCount:      2,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
					defaults.RackTopologyKey: []string{
						"rack0",
						"rack1",
						"rack2",
					},
				},
			},
		},
		{
			label:          "Case 3", // three nodes spread across three zones
			nodeList:       &corev1.NodeList{},
			storageCluster: &api.StorageCluster{},
			zoneCount:      3,
			expectedNodeTopologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
					hostnameLabel: []string{
						"node1",
						"node2",
						"node3",
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		mockStorageCluster.DeepCopyInto(tc.storageCluster)
		mockNodeList.DeepCopyInto(tc.nodeList)
		if tc.zoneCount == 2 {
			tc.nodeList.Items[2].Labels[zoneTopologyLabel] = "zone2"
		} else if tc.zoneCount == 1 {
			tc.nodeList.Items[1].Labels[zoneTopologyLabel] = "zone1"
			tc.nodeList.Items[2].Labels[zoneTopologyLabel] = "zone1"
		}

		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster, tc.nodeList)
		err := reconciler.reconcileNodeTopologyMap(tc.storageCluster)
		assert.NoError(t, err)

		err = reconciler.Client.Status().Update(context.TODO(), tc.storageCluster)
		assert.NoError(t, err)

		actual := &api.StorageCluster{}
		err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedNodeTopologyMap, actual.Status.NodeTopologies, "[%s]: failed to get correct nodeToplogies", tc.label)

	}
}

func TestReconcileNodeTopologyMapFailure(t *testing.T) {
	testcases := []struct {
		label          string
		nodeList       *corev1.NodeList
		storageCluster *api.StorageCluster
		nodesAvailable bool
		repilcaCount   int
	}{
		{
			label:          "Case 1", // deviceSet replica count (4) greater than eligible nodes (3)
			nodeList:       &corev1.NodeList{},
			storageCluster: &api.StorageCluster{},
			nodesAvailable: true,
			repilcaCount:   4,
		},
		{
			label:          "Case 2", // No eligible nodes found
			nodeList:       &corev1.NodeList{},
			storageCluster: &api.StorageCluster{},
			nodesAvailable: false,
			repilcaCount:   3,
		},
	}

	for _, tc := range testcases {
		mockStorageCluster.DeepCopyInto(tc.storageCluster)
		tc.storageCluster.Spec.StorageDeviceSets = mockDeviceSets
		if tc.nodesAvailable {
			mockNodeList.DeepCopyInto(tc.nodeList)
		}
		tc.storageCluster.Spec.StorageDeviceSets[0].Replica = tc.repilcaCount
		reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, tc.nodeList)
		err := reconciler.reconcileNodeTopologyMap(tc.storageCluster)
		assert.Errorf(t, err, "[%s]: failed to test ReconcileNodeTopologyMap failure condition", tc.label)
	}
}

func TestFailureDomain(t *testing.T) {
	testcases := []struct {
		label                 string
		storageCluster        *api.StorageCluster
		NodeTopologyMap       *api.NodeTopologyMap
		expectedFailureDomain string
	}{
		{
			label: "Case 1", // storagecluster has predefined failure domain of `zone`
			storageCluster: &api.StorageCluster{
				Status: api.StorageClusterStatus{
					FailureDomain:  "zone",
					NodeTopologies: ocsv1.NewNodeTopologyMap(),
				},
			},
			expectedFailureDomain: "zone",
		},
		{
			label: "Case 2", // storagecluster has predefined failure domain of `rack`
			storageCluster: &api.StorageCluster{
				Status: api.StorageClusterStatus{
					FailureDomain:  "rack",
					NodeTopologies: ocsv1.NewNodeTopologyMap(),
				},
			},
			expectedFailureDomain: "rack",
		},
		{
			label: "Case 3", // storagecluster with three or more zone topology labels
			storageCluster: &api.StorageCluster{
				Status: api.StorageClusterStatus{
					NodeTopologies: &api.NodeTopologyMap{
						Labels: map[string]api.TopologyLabelValues{
							zoneTopologyLabel: []string{
								"zone1",
								"zone2",
								"zone3",
							},
						},
					},
				},
			},
			expectedFailureDomain: "zone",
		},
		{
			label: "Case 4", // storagecluster with less than three zone topology labels
			storageCluster: &api.StorageCluster{
				Status: api.StorageClusterStatus{
					NodeTopologies: &api.NodeTopologyMap{
						Labels: map[string]api.TopologyLabelValues{
							zoneTopologyLabel: []string{
								"zone1",
								"zone2",
							},
						},
					},
				},
			},
			expectedFailureDomain: "rack",
		},
		{
			label: "Case 5", // storagecluster has predefined failure domain of `host`
			storageCluster: &api.StorageCluster{
				Status: api.StorageClusterStatus{
					FailureDomain:  "host",
					NodeTopologies: ocsv1.NewNodeTopologyMap(),
				},
			},
			expectedFailureDomain: "host",
		},
		{
			label: "Case 6", // storagecluster with FlexibleScaling enabled
			storageCluster: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					FlexibleScaling: true,
				},
				Status: api.StorageClusterStatus{
					NodeTopologies: ocsv1.NewNodeTopologyMap(),
				},
			},
			expectedFailureDomain: "host",
		},
	}

	for _, tc := range testcases {
		failureDomain := determineFailureDomain(tc.storageCluster).Type
		assert.Equalf(t, tc.expectedFailureDomain, failureDomain, "[%s]: failed to get correct failure domain", tc.label)
	}
}

func TestStorageClusterEligibleNodes(t *testing.T) {
	testcases := []struct {
		label             string
		storageCluster    *api.StorageCluster
		nodeList          *corev1.NodeList
		labelSelectors    *metav1.LabelSelector
		expectedNodeCount int
	}{
		{
			label:          "Case 1", // One eligible node with `WorkerAffinityKey` matching storageCluster labelselector
			storageCluster: &api.StorageCluster{},
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{
					Kind: "NodeList",
				},
				Items: []corev1.Node{
					workerAffinityNode,
				},
			},
			labelSelectors: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      WorkerAffinityKey,
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
			expectedNodeCount: 1,
		},
		{
			label:          "Case 2", // One out of two nodes match default NodeAffinityKey
			storageCluster: &api.StorageCluster{},
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{
					Kind: "NodeList",
				},
				Items: []corev1.Node{
					workerAffinityNode,
					defaultAffinityNode,
				},
			},
			labelSelectors:    nil,
			expectedNodeCount: 1,
		},
		{
			label:          "Case 3", // No eligible nodes matching default NodeAffinityKey
			storageCluster: &api.StorageCluster{},
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{
					Kind: "NodeList",
				},
				Items: []corev1.Node{
					workerAffinityNode,
				},
			},
			labelSelectors:    nil,
			expectedNodeCount: 0,
		},
	}

	for _, tc := range testcases {
		mockStorageCluster.DeepCopyInto(tc.storageCluster)
		tc.storageCluster.Spec.LabelSelector = tc.labelSelectors
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster, tc.nodeList)
		actualNodes, err := reconciler.getStorageClusterEligibleNodes(tc.storageCluster)
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedNodeCount, len(actualNodes.Items), "[%s]: failed to get eligible nodes", tc.label)
	}
}

func TestEnsureNodeRack(t *testing.T) {
	testcases := []struct {
		label       string
		nodeList    *corev1.NodeList
		minRacks    int
		nodeRacks   *ocsv1.NodeTopologyMap
		topologyMap *ocsv1.NodeTopologyMap
	}{
		{
			label: "Case 1", // ensure correct rack labels are added to all the nodes
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{
					Kind: "NodeList",
				},
				Items: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "Node1",
							Labels: map[string]string{},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "Node2",
							Labels: map[string]string{},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "Node3",
							Labels: map[string]string{},
						},
					},
				},
			},
			minRacks: 3,
			nodeRacks: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{},
			},
			topologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
				},
			},
		},
		{
			label: "Case 2", // ensure rack2 label is added to Node3
			nodeList: &corev1.NodeList{
				TypeMeta: metav1.TypeMeta{
					Kind: "NodeList",
				},
				Items: []corev1.Node{
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node1",
							Labels: map[string]string{
								defaults.RackTopologyKey: "rack0",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name: "Node2",
							Labels: map[string]string{
								defaults.RackTopologyKey: "rack1",
							},
						},
					},
					{
						ObjectMeta: metav1.ObjectMeta{
							Name:   "Node3",
							Labels: map[string]string{},
						},
					},
				},
			},
			minRacks: 3,
			nodeRacks: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					rack0: []string{
						"Node1",
					},
					rack1: []string{
						"Node2",
					},
				},
			},
			topologyMap: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		reconciler := createFakeStorageClusterReconciler(t, tc.nodeList)
		err := reconciler.ensureNodeRacks(tc.nodeList, tc.minRacks, tc.nodeRacks, tc.topologyMap)
		assert.NoError(t, err)

		actualNodeList := &corev1.NodeList{}
		err = reconciler.Client.List(context.TODO(), actualNodeList)
		assert.NoError(t, err)
		for i, node := range actualNodeList.Items {
			for key, value := range node.Labels {
				assert.Containsf(t, key, defaults.RackTopologyKey, "[%s]: failed to added rack label", tc.label)
				assert.Containsf(t, value, fmt.Sprintf("rack%d", i), "[%s]: failed to added rack label", tc.label)
			}
		}

	}
}
func TestDeterminePlacementRack(t *testing.T) {
	testcases := []struct {
		label        string
		nodeList     *corev1.NodeList
		node         corev1.Node
		minRacks     int
		nodeRacks    *ocsv1.NodeTopologyMap
		expectedRack string
	}{
		{
			label:    "Case 1", // `rack0` should be placement rack as `nodeRacks` is empty
			nodeList: &corev1.NodeList{},
			node:     corev1.Node{},
			minRacks: 3,
			nodeRacks: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{},
			},
			expectedRack: "rack0",
		},
		{
			label:    "Case 2", // `rack1` should be placement rack as `nodeRacks` already has `rack0`
			nodeList: &corev1.NodeList{},
			node:     corev1.Node{},
			minRacks: 3,
			nodeRacks: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					rack0: []string{
						"node1",
						"node2",
						"node3",
					},
				},
			},
			expectedRack: "rack1",
		},
		{
			label:    "Case 3", // `rack2` should be placement rack as `nodeRacks` already has `rack0` and `rack1`
			nodeList: &corev1.NodeList{},
			node:     corev1.Node{},
			minRacks: 3,
			nodeRacks: &api.NodeTopologyMap{
				Labels: map[string]api.TopologyLabelValues{
					rack0: []string{
						"node1",
						"node2",
						"node3",
					},
					rack1: []string{
						"node1",
						"node2",
						"node3",
					},
				},
			},
			expectedRack: "rack2",
		},
	}

	for _, tc := range testcases {
		mockNodeList.DeepCopyInto(tc.nodeList)
		tc.node = tc.nodeList.Items[0]
		actual := determinePlacementRack(tc.nodeList, tc.node, tc.minRacks, tc.nodeRacks)
		assert.Equalf(t, tc.expectedRack, actual, "[%s]: failed to get correct placement rack", tc.label)
	}
}
