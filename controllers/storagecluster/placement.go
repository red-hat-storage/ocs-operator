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

// getPlacement returns placement configuration for ceph components with appropriate topology
func getPlacement(sc *ocsv1.StorageCluster, component string) rookCephv1.Placement {
	placement := rookCephv1.Placement{}
	in, ok := sc.Spec.Placement[rookCephv1.KeyType(component)]
	if ok {
		(&in).DeepCopyInto(&placement)
	} else {
		in := defaults.DaemonPlacements[component]
		(&in).DeepCopyInto(&placement)
		// label rook_file_system is added to the mds pod using rook operator
		if component == "mds" {
			// if active MDS number is more than 1 then Preferred and if it is 1 then Required pod anti-affinity is set
			mdsWeightedPodAffinity := defaults.GetMdsWeightedPodAffinityTerm(100, GenerateNameForCephFilesystem(sc))
			if sc.Spec.ManagedResources.CephFilesystems.ActiveMetadataServers > 1 {
				placement.PodAntiAffinity = &corev1.PodAntiAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						mdsWeightedPodAffinity,
					},
				}
			} else {
				placement.PodAntiAffinity = &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						mdsWeightedPodAffinity.PodAffinityTerm,
					},
				}
			}
		}
	}

	// ignore default PodAntiAffinity mon placement when arbiter is enabled
	if component == "mon" && arbiterEnabled(sc) {
		placement.PodAntiAffinity = &corev1.PodAntiAffinity{}
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

	// if provider-server placements are found in the storagecluster spec append the default ocs tolerations to it
	if ok && component == defaults.APIServerKey {
		placement.Tolerations = append(placement.Tolerations, defaults.DaemonPlacements[component].Tolerations...)
		return placement
	}

	// if metrics-exporter placements are found in the storagecluster spec append the default ocs tolerations to it
	if ok && component == defaults.MetricsExporterKey {
		placement.Tolerations = append(placement.Tolerations, defaults.DaemonPlacements[component].Tolerations...)
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

	topologyMap := sc.Status.NodeTopologies
	if topologyMap == nil {
		return placement
	}

	topologyKey := getFailureDomain(sc)
	topologyKey, _ = topologyMap.GetKeyValues(topologyKey)
	if component == "mon" || component == "mds" || component == "rgw" {
		if placement.PodAntiAffinity != nil {
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
