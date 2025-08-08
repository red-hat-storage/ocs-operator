package storagecluster

import (
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// getPlacement returns placement configuration for ceph components with appropriate topology
func getPlacement(sc *ocsv1.StorageCluster, component string) rookCephv1.Placement {
	defaultPlacement := defaults.DaemonPlacements[component]
	specifiedPlacement, specified := sc.Spec.Placement[rookCephv1.KeyType(component)]
	placement := mergePlacements(defaultPlacement, specified, specifiedPlacement)

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
		// Don't add node affinity again for these rook-ceph daemons as it is already added via the "all" key
		if component != "mgr" && component != "mon" && component != "osd" && component != "osd-prepare" {
			placement.NodeAffinity = defaults.DefaultNodeAffinity
		}
	}

	// If the StorageCluster specifies a label selector, append it to the
	// node affinity, creating it if it doesn't exist.
	if sc.Spec.LabelSelector != nil {
		reqs := convertLabelToNodeSelectorRequirements(*sc.Spec.LabelSelector)
		if len(reqs) != 0 {
			appendNodeRequirements(&placement, reqs...)
		}
	}

	topologyKey := sc.Status.FailureDomainKey
	// Change the topology key to the failure domain key, osd & osd-prepare are handled separately
	if component == "mgr" || component == "mon" {
		for i := range placement.TopologySpreadConstraints {
			placement.TopologySpreadConstraints[i].TopologyKey = topologyKey
		}
	}
	if component == "mds" {
		placement.TopologySpreadConstraints[0].TopologyKey = topologyKey
		placement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions[0].Values = []string{util.GenerateNameForCephFilesystem(sc.Name)}
	}
	if component == "rgw" {
		placement.TopologySpreadConstraints[0].TopologyKey = topologyKey
		placement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions[0].Values = []string{util.GenerateNameForCephObjectStore(sc)}
	}
	if component == "nfs" {
		placement.TopologySpreadConstraints[0].TopologyKey = topologyKey
		placement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions[0].Values = []string{util.GenerateNameForCephNFS(sc)}
	}

	// No need of TSC for single node deployment
	if statusutil.IsSingleNodeDeployment() {
		placement.TopologySpreadConstraints = nil
	}

	return placement
}

// mergePlacements merges the default and user-specified placements
func mergePlacements(defaultPlacement rookCephv1.Placement, specified bool, specifiedPlacement rookCephv1.Placement) rookCephv1.Placement {
	merged := rookCephv1.Placement{}

	// NodeAffinity
	if specified && specifiedPlacement.NodeAffinity != nil {
		merged.NodeAffinity = specifiedPlacement.NodeAffinity.DeepCopy()
	} else if defaultPlacement.NodeAffinity != nil {
		merged.NodeAffinity = defaultPlacement.NodeAffinity.DeepCopy()
	}

	// PodAffinity
	if specified && specifiedPlacement.PodAffinity != nil {
		merged.PodAffinity = specifiedPlacement.PodAffinity.DeepCopy()
	} else if defaultPlacement.PodAffinity != nil {
		merged.PodAffinity = defaultPlacement.PodAffinity.DeepCopy()
	}

	// PodAntiAffinity
	if specified && specifiedPlacement.PodAntiAffinity != nil {
		merged.PodAntiAffinity = specifiedPlacement.PodAntiAffinity.DeepCopy()
	} else if defaultPlacement.PodAntiAffinity != nil {
		merged.PodAntiAffinity = defaultPlacement.PodAntiAffinity.DeepCopy()
	}

	// Tolerations: append specified to default
	tolerations := []corev1.Toleration{}
	if len(defaultPlacement.Tolerations) > 0 {
		tolerations = append(tolerations, defaultPlacement.Tolerations...)
	}
	if specified {
		tolerations = append(tolerations, specifiedPlacement.Tolerations...)
	}
	merged.Tolerations = removeDuplicateTolerations(tolerations)

	// TopologySpreadConstraints: append specified to default
	topologySpreadConstraints := []corev1.TopologySpreadConstraint{}
	if len(defaultPlacement.TopologySpreadConstraints) > 0 {
		topologySpreadConstraints = append(topologySpreadConstraints, defaultPlacement.TopologySpreadConstraints...)
	}
	if specified {
		topologySpreadConstraints = append(topologySpreadConstraints, specifiedPlacement.TopologySpreadConstraints...)
	}
	merged.TopologySpreadConstraints = removeDuplicateTSC(topologySpreadConstraints)

	return merged
}

// removeDuplicateTolerations removes duplicate tolerations
func removeDuplicateTolerations(tolerations []corev1.Toleration) []corev1.Toleration {
	if len(tolerations) == 0 {
		return nil
	}
	if len(tolerations) == 1 {
		return tolerations
	}
	seen := make(map[string]bool, len(tolerations))
	result := make([]corev1.Toleration, 0, len(tolerations))
	for _, t := range tolerations {
		key := t.Key + "|" + string(t.Operator) + "|" + t.Value + "|" + string(t.Effect)
		if !seen[key] {
			seen[key] = true
			result = append(result, t)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// removeDuplicateTSC removes duplicate topology spread constraints
func removeDuplicateTSC(constraints []corev1.TopologySpreadConstraint) []corev1.TopologySpreadConstraint {
	if len(constraints) == 0 {
		return nil
	}
	if len(constraints) == 1 {
		return constraints
	}
	seen := make(map[string]bool, len(constraints))
	result := make([]corev1.TopologySpreadConstraint, 0, len(constraints))
	for _, c := range constraints {
		key := c.TopologyKey + "|" + string(c.WhenUnsatisfiable) + "|" + strconv.Itoa(int(c.MaxSkew))
		if !seen[key] {
			seen[key] = true
			result = append(result, c)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
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
