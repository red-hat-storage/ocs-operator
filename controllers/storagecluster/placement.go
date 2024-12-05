package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getPlacement returns placement configuration for specified component
func getPlacement(sc *ocsv1.StorageCluster, component string) rookCephv1.Placement {
	// Fetch placement spec specified for the component from the StorageCluster CR
	placement := sc.Spec.Placement[rookCephv1.KeyType(component)]

	// If no node affinity is specified for the given component and the StorageCluster has no label selector
	// Set the default node affinity.
	if placement.NodeAffinity == nil && sc.Spec.LabelSelector == nil {
		placement.NodeAffinity = defaults.DefaultNodeAffinity
	}

	// If the StorageCluster specifies a label selector, append it to the
	// node affinity, creating it if it doesn't exist.
	if sc.Spec.LabelSelector != nil {
		reqs := convertLabelToNodeSelectorRequirements(*sc.Spec.LabelSelector)
		if len(reqs) != 0 {
			appendNodeRequirements(&placement, reqs...)
		}
	}

	// Add the ocs toleration to the placement of any component
	placement.Tolerations = append(placement.Tolerations, defaults.GetOcsToleration())

	// Add the master node toleration to the placement of the arbiter component if it is not disabled
	if component == "arbiter" {
		if !sc.Spec.Arbiter.DisableMasterNodeToleration {
			placement.Tolerations = append(placement.Tolerations, corev1.Toleration{
				Key:      "node-role.kubernetes.io/master",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			})
		}
	}

	// for these ceph-daemons we always need to add topology spread constraints if not present, to ensure their even distribution
	if component == "mgr" || component == "mon" || component == "osd" || component == "osd-prepare" || component == "mds" || component == "rgw" || component == "nfs" {
		if len(placement.TopologySpreadConstraints) == 0 {
			placement.TopologySpreadConstraints = defaults.DaemonPlacements[component].TopologySpreadConstraints
		}
		// if the topology key is empty, set it to the failure domain key of the cluster
		for i := range placement.TopologySpreadConstraints {
			if placement.TopologySpreadConstraints[i].TopologyKey == "" {
				placement.TopologySpreadConstraints[i].TopologyKey = sc.Status.FailureDomainKey
			}
		}
	}

	return placement
}

// convertLabelToNodeSelectorRequirements returns a NodeSelectorRequirement list from a given LabelSelector
func convertLabelToNodeSelectorRequirements(labelSelector metav1.LabelSelector) []corev1.NodeSelectorRequirement {
	reqs := []corev1.NodeSelectorRequirement{}
	for key, value := range labelSelector.MatchLabels {
		req := corev1.NodeSelectorRequirement{}
		req.Key = key
		req.Operator = corev1.NodeSelectorOpIn
		req.Values = append(req.Values, value)
		reqs = append(reqs, req)
	}
	numIter := len(labelSelector.MatchExpressions)
	for i := 0; i < numIter; i++ {
		req := corev1.NodeSelectorRequirement{}
		req.Key = labelSelector.MatchExpressions[i].Key
		req.Operator = corev1.NodeSelectorOperator(labelSelector.MatchExpressions[i].Operator)
		req.Values = labelSelector.MatchExpressions[i].Values
		reqs = append(reqs, req)
	}
	return reqs
}

func appendNodeRequirements(placement *rookCephv1.Placement, reqs ...corev1.NodeSelectorRequirement) {
	if placement.NodeAffinity == nil {
		placement.NodeAffinity = &corev1.NodeAffinity{}
	}
	if placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution == nil {
		placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = &corev1.NodeSelector{}
	}
	nodeSelector := placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution
	if len(nodeSelector.NodeSelectorTerms) == 0 {
		nodeSelector.NodeSelectorTerms = append(nodeSelector.NodeSelectorTerms, corev1.NodeSelectorTerm{})
	}
	nodeSelector.NodeSelectorTerms[0].MatchExpressions = append(nodeSelector.NodeSelectorTerms[0].MatchExpressions, reqs...)
}

// MatchingLabelsSelector filters the list/delete operation on the given label
// selector (or index in the case of cached lists). A struct is used because
// labels.Selector is an interface, which cannot be aliased.
type MatchingLabelsSelector struct {
	labels.Selector
}

// ApplyToList applies this configuration to the given list options.
// This is implemented by MatchingLabelsSelector which implements ListOption interface.
func (m MatchingLabelsSelector) ApplyToList(opts *client.ListOptions) {
	opts.LabelSelector = m
}
