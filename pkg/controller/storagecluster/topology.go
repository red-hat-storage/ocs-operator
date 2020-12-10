package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/go-logr/logr"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (r *ReconcileStorageCluster) getStorageClusterEligibleNodes(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (nodes *corev1.NodeList, err error) {
	nodes = &corev1.NodeList{}
	var selector labels.Selector

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{defaults.NodeAffinityKey: ""},
	}
	if sc.Spec.LabelSelector != nil {
		labelSelector = sc.Spec.LabelSelector
	}

	selector, err = metav1.LabelSelectorAsSelector(labelSelector)
	if err != nil {
		return nodes, err
	}
	err = r.client.List(context.TODO(), nodes, MatchingLabelsSelector{Selector: selector})

	return nodes, err
}

// determineFailureDomain determines the appropriate Ceph failure domain based
// on the storage cluster's topology map
func determineFailureDomain(sc *ocsv1.StorageCluster) string {
	if sc.Status.FailureDomain != "" {
		return sc.Status.FailureDomain
	}

	if sc.Spec.FlexibleScaling {
		return "host"
	}

	topologyMap := sc.Status.NodeTopologies
	failureDomain := "rack"
	for label, labelValues := range topologyMap.Labels {
		if strings.Contains(label, "zone") {
			if (len(labelValues) >= 2 && arbiterEnabled(sc)) || (len(labelValues) >= 3) {
				failureDomain = "zone"
			}
		}
	}
	return failureDomain
}

// determinePlacementRack sorts the list of known racks in alphabetical order,
// counts the number of Nodes in each rack, then returns the first rack with
// the fewest number of Nodes. If there are fewer than three racks, define new
// racks so that there are at least three. It also ensures that only racks with
// either no nodes or nodes in the same AZ are considered valid racks.
func determinePlacementRack(
	nodes *corev1.NodeList, node corev1.Node,
	minRacks int, nodeRacks *ocsv1.NodeTopologyMap) string {

	rackList := []string{}

	if len(nodeRacks.Labels) < minRacks {
		for i := len(nodeRacks.Labels); i < minRacks; i++ {
			for j := 0; j <= i; j++ {
				newRack := fmt.Sprintf("rack%d", j)
				if _, ok := nodeRacks.Labels[newRack]; !ok {
					nodeRacks.Labels[newRack] = ocsv1.TopologyLabelValues{}
					break
				}
			}
		}
	}

	targetAZ := ""
	for label, value := range node.Labels {
		for _, key := range validTopologyLabelKeys {
			if strings.Contains(label, key) && strings.Contains(label, "zone") {
				targetAZ = value
				break
			}
		}
		if targetAZ != "" {
			break
		}
	}

	if len(targetAZ) > 0 {
		for rack := range nodeRacks.Labels {
			nodeNames := nodeRacks.Labels[rack]
			if len(nodeNames) == 0 {
				rackList = append(rackList, rack)
				continue
			}

			validRack := false
			for _, nodeName := range nodeNames {
				for _, n := range nodes.Items {
					if n.Name == nodeName {
						for label, value := range n.Labels {
							for _, key := range validTopologyLabelKeys {
								if strings.Contains(label, key) && strings.Contains(label, "zone") && value == targetAZ {
									validRack = true
									break
								}
							}
							if validRack {
								break
							}
						}
						break
					}
				}
				if validRack {
					break
				}
			}
			if validRack {
				rackList = append(rackList, rack)
			}
		}
	} else {
		for rack := range nodeRacks.Labels {
			rackList = append(rackList, rack)
		}
	}

	sort.Strings(rackList)
	rack := rackList[0]

	for _, r := range rackList {
		if len(nodeRacks.Labels[r]) < len(nodeRacks.Labels[rack]) {
			rack = r
		}
	}

	return rack
}

func generateStrategicPatch(oldObj, newObj interface{}) (client.Patch, error) {
	oldJSON, err := json.Marshal(oldObj)
	if err != nil {
		return nil, err
	}

	newJSON, err := json.Marshal(newObj)
	if err != nil {
		return nil, err
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, oldObj)
	if err != nil {
		return nil, err
	}

	return client.RawPatch(types.StrategicMergePatchType, patch), nil
}

// ensureNodeRacks iterates through the list of storage nodes and ensures
// all nodes have a rack topology label.
func (r *ReconcileStorageCluster) ensureNodeRacks(
	nodes *corev1.NodeList, minRacks int,
	nodeRacks, topologyMap *ocsv1.NodeTopologyMap,
	reqLogger logr.Logger) error {

	for _, node := range nodes.Items {
		hasRack := false

		for _, nodeNames := range nodeRacks.Labels {
			for _, nodeName := range nodeNames {
				if nodeName == node.Name {
					hasRack = true
					break
				}
			}
			if hasRack {
				break
			}
		}

		if !hasRack {
			rack := determinePlacementRack(nodes, node, minRacks, nodeRacks)
			nodeRacks.Add(rack, node.Name)
			if !topologyMap.Contains(defaults.RackTopologyKey, rack) {
				reqLogger.Info("Adding rack label from node", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
				topologyMap.Add(defaults.RackTopologyKey, rack)
			}

			reqLogger.Info("Labeling node with rack label", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
			newNode := node.DeepCopy()
			newNode.Labels[defaults.RackTopologyKey] = rack
			patch, err := generateStrategicPatch(node, newNode)
			if err != nil {
				return err
			}
			err = r.client.Patch(context.TODO(), &node, patch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// reconcileNodeTopologyMap builds the map of all topology labels on all nodes
// in the storage cluster
func (r *ReconcileStorageCluster) reconcileNodeTopologyMap(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	minNodes := getMinimumNodes(sc)

	for _, deviceSet := range sc.Spec.StorageDeviceSets {
		if deviceSet.Replica > minNodes {
			minNodes = deviceSet.Replica
		}
	}

	nodes, err := r.getStorageClusterEligibleNodes(sc, reqLogger)
	if err != nil {
		return err
	}

	if sc.Status.NodeTopologies == nil || sc.Status.NodeTopologies.Labels == nil {
		sc.Status.NodeTopologies = ocsv1.NewNodeTopologyMap()
	}
	topologyMap := sc.Status.NodeTopologies
	nodeRacks := ocsv1.NewNodeTopologyMap()

	r.nodeCount = len(nodes.Items)

	if r.nodeCount < minNodes {
		return fmt.Errorf("Not enough nodes found: Expected %d, found %d", minNodes, r.nodeCount)
	}

	for _, node := range nodes.Items {
		labels := node.Labels
		for label, value := range labels {
			for _, key := range validTopologyLabelKeys {
				if strings.Contains(label, key) {
					if !topologyMap.Contains(label, value) {
						reqLogger.Info("Adding topology label from node", "Node", node.Name, "Label", label, "Value", value)
						topologyMap.Add(label, value)
					}
				}
			}
			if strings.Contains(label, "rack") {
				if !nodeRacks.Contains(value, node.Name) {
					nodeRacks.Add(value, node.Name)
				}
			}
		}

	}

	if determineFailureDomain(sc) == "rack" {
		err = r.ensureNodeRacks(nodes, minNodes, nodeRacks, topologyMap, reqLogger)
		if err != nil {
			return err
		}
	}

	return nil
}
