package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type ocsTopologyMap struct{}

var validTopologyLabelKeys = []string{
	corev1.LabelTopologyRegion,
	corev1.LabelTopologyZone,
	corev1.LabelHostname,
	defaults.RackTopologyKey,
}

// ensureCreated ensures that StorageCluster.Status.Topology is up to date
func (obj *ocsTopologyMap) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if err := r.reconcileNodeTopologyMap(instance); err != nil {
		r.Log.Error(err, "Failed to set node Topology Map for StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsTopologyMap
func (obj *ocsTopologyMap) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func (r *StorageClusterReconciler) getStorageClusterEligibleNodes(sc *ocsv1.StorageCluster) (nodes *corev1.NodeList, err error) {
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
	err = r.Client.List(context.TODO(), nodes, MatchingLabelsSelector{Selector: selector})

	return nodes, err
}

// getFailureDomain returns the failure domain that was determined at the time of node topology reconciliation
func getFailureDomain(sc *ocsv1.StorageCluster) string {
	return sc.Status.FailureDomain
}

// setFailureDomain determines the appropriate Ceph failure domain based
// on the storage cluster's topology map
func setFailureDomain(sc *ocsv1.StorageCluster) {

	// We don't change the failure domain after it is determined
	if sc.Status.FailureDomain != "" {
		sc.Status.FailureDomainKey, sc.Status.FailureDomainValues = sc.Status.NodeTopologies.GetKeyValues(sc.Status.FailureDomain)
		return
	}

	// default is rack
	failureDomain := "rack"

	if statusutil.IsSingleNodeDeployment() {
		sc.Status.FailureDomain = "osd"
		// Since nodes do not have a label for "osd" as a failure domain, setting it to "host".
		sc.Status.FailureDomainKey, sc.Status.FailureDomainValues = sc.Status.NodeTopologies.GetKeyValues("host")
		return
	}

	// But if FlexiableScaling is enabled then we select host as failure domain
	// as we need +1 scaling
	if sc.Spec.FlexibleScaling {
		failureDomain = "host"
		sc.Status.FailureDomain = failureDomain
		sc.Status.FailureDomainKey, sc.Status.FailureDomainValues = sc.Status.NodeTopologies.GetKeyValues(sc.Status.FailureDomain)
		return
	}

	// If sufficient zones are available then we select zone as the failure domain
	topologyMap := sc.Status.NodeTopologies
	for label, labelValues := range topologyMap.Labels {
		if label == corev1.LabelZoneFailureDomainStable {
			if (len(labelValues) >= 2 && arbiterEnabled(sc)) || (len(labelValues) >= 3) {
				failureDomain = "zone"
			}
		}
	}

	sc.Status.FailureDomain = failureDomain
	sc.Status.FailureDomainKey, sc.Status.FailureDomainValues = sc.Status.NodeTopologies.GetKeyValues(sc.Status.FailureDomain)
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
		for i := 0; i < minRacks; i++ {
			newRack := fmt.Sprintf("rack%d", i)
			if _, ok := nodeRacks.Labels[newRack]; !ok {
				nodeRacks.Labels[newRack] = ocsv1.TopologyLabelValues{}
				break
			}
		}
	}

	targetAZ := ""
	for label, value := range node.Labels {
		for _, key := range validTopologyLabelKeys {
			if strings.Contains(label, key) && (label == corev1.LabelZoneFailureDomainStable) {
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
								if strings.Contains(label, key) && (label == corev1.LabelZoneFailureDomainStable) && value == targetAZ {
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
		if len(rackList) == 0 {
			//Create a new rack because EBS volumes cannot move to a different
			// AZ
			for i := len(nodeRacks.Labels); ; i++ {
				newRack := fmt.Sprintf("rack%d", i)
				if _, ok := nodeRacks.Labels[newRack]; !ok {
					nodeRacks.Labels[newRack] = ocsv1.TopologyLabelValues{}
					rackList = append(rackList, newRack)
					break
				}
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
func (r *StorageClusterReconciler) ensureNodeRacks(
	nodes *corev1.NodeList, minRacks int,
	nodeRacks, topologyMap *ocsv1.NodeTopologyMap) error {

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
				r.Log.Info("Adding rack label from Node.", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
				topologyMap.Add(defaults.RackTopologyKey, rack)
			}

			r.Log.Info("Labeling node with rack label.", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
			newNode := node.DeepCopy()
			newNode.Labels[defaults.RackTopologyKey] = rack
			patch, err := generateStrategicPatch(node, newNode)
			if err != nil {
				return err
			}
			err = r.Client.Patch(context.TODO(), &node, patch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// reconcileNodeTopologyMap builds the map of all topology labels on all nodes
// in the storage cluster
func (r *StorageClusterReconciler) reconcileNodeTopologyMap(sc *ocsv1.StorageCluster) error {
	minNodes := getMinimumNodes(sc, r.isTnfCluster)

	nodes, err := r.getStorageClusterEligibleNodes(sc)
	if err != nil {
		return err
	}

	topologyMap := ocsv1.NewNodeTopologyMap()
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

	sortTopologyMapLabelValues(topologyMap)
	sc.Status.NodeTopologies = topologyMap
	setFailureDomain(sc)

	if getFailureDomain(sc) == "rack" {
		err = r.ensureNodeRacks(nodes, minNodes, nodeRacks, topologyMap)
		if err != nil {
			return err
		}
	}

	return nil
}

// A function to sort the values of a label in the topology map
func sortTopologyMapLabelValues(topologyMap *ocsv1.NodeTopologyMap) {
	for label, values := range topologyMap.Labels {
		sort.Strings(values)
		topologyMap.Labels[label] = values
	}
}
