package storagecluster

import (
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
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
	} else if !ok && (len(sc.Spec.Placement) == 0) {
		in := defaults.DaemonPlacements[component]
		(&in).DeepCopyInto(&placement)
	}
	term := convertLabelToNodeSelector(sc.Spec.LabelSelector)
	if len(term.MatchExpressions) != 0 {
		placement.NodeAffinity = &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{term},
			},
		}
	}
	topologyMap := sc.Status.NodeTopologies
	if topologyMap != nil && (component == "mon" || component == "mds") {
		topologyKey := determineFailureDomain(sc)
		topologyKey, _ = topologyMap.GetKeyValues(topologyKey)
		podAffinityTerms := placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
		podAffinityTerms[0].PodAffinityTerm.TopologyKey = topologyKey
	}
	return placement
}

//convertLabelToNodeSelector returns NodeSelectorTerm type from a given LabelSelector
func convertLabelToNodeSelector(labelSelector metav1.LabelSelector) corev1.NodeSelectorTerm {
	term := corev1.NodeSelectorTerm{}
	for key, value := range labelSelector.MatchLabels {
		req := corev1.NodeSelectorRequirement{}
		req.Key = key
		req.Operator = corev1.NodeSelectorOpIn
		req.Values = append(req.Values, value)
		term.MatchExpressions = append(term.MatchExpressions, req)
	}
	numIter := len(labelSelector.MatchExpressions)
	for i := 0; i < numIter; i++ {
		req := corev1.NodeSelectorRequirement{}
		req.Key = labelSelector.MatchExpressions[i].Key
		req.Operator = corev1.NodeSelectorOperator(labelSelector.MatchExpressions[i].Operator)
		req.Values = labelSelector.MatchExpressions[i].Values
		term.MatchExpressions = append(term.MatchExpressions, req)
	}
	return term
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
