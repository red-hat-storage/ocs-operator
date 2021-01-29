package storagecluster

import (
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	rookv1 "github.com/rook/rook/pkg/apis/rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getPlacement returns placement configuration for ceph components with appropriate topology
func getPlacement(sc *ocsv1.StorageCluster, component string) rookv1.Placement {
	placement := rookv1.Placement{}
	in, ok := sc.Spec.Placement[rookv1.KeyType(component)]
	if ok {
		(&in).DeepCopyInto(&placement)
	} else {
		in := defaults.DaemonPlacements[component]
		(&in).DeepCopyInto(&placement)
	}

	if component == "arbiter" {
		if !sc.Spec.Arbiter.DisableMasterNodeToleration {
			placement.Tolerations = append(placement.Tolerations, corev1.Toleration{
				Key:      "node-role.kubernetes.io/master",
				Operator: corev1.TolerationOpExists,
				Effect:   corev1.TaintEffectNoSchedule,
			})
		}
		return placement
	}

	// If no placement is specified for the given component and the
	// StorageCluster has no label selector, set the default node
	// affinity.
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

	topologyMap := sc.Status.NodeTopologies
	if topologyMap == nil {
		return placement
	}

	topologyKey := determineFailureDomain(sc).Key
	if component == "mon" || component == "mds" {
		if placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution {
				placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[i].PodAffinityTerm.TopologyKey = topologyKey
			}
		}
		if placement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution != nil {
			for i := range placement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution {
				placement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[i].TopologyKey = topologyKey
			}
		}
	}

	return placement
}

//convertLabelToNodeSelectorRequirements returns a NodeSelectorRequirement list from a given LabelSelector
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

func appendNodeRequirements(placement *rookv1.Placement, reqs ...corev1.NodeSelectorRequirement) {
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
//This is implemented by MatchingLabelsSelector which implements ListOption interface.
func (m MatchingLabelsSelector) ApplyToList(opts *client.ListOptions) {
	opts.LabelSelector = m
}

// setTopologyForAffinity assigns topology related values to the affinity placements
func setTopologyForAffinity(placement *rookv1.Placement, selectorValue string, topologyKey string) {
	placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey = topologyKey

	nodeZoneSelector := corev1.NodeSelectorRequirement{
		Key:      topologyKey,
		Operator: corev1.NodeSelectorOpIn,
		Values:   []string{selectorValue},
	}
	appendNodeRequirements(placement, nodeZoneSelector)
}
