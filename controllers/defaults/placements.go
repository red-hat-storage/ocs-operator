package defaults

import (
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	APIServerKey       = "api-server"
	MetricsExporterKey = "metrics-exporter"
	CsiPluginKey       = "csi-plugin"
	CsiProvisionerKey  = "csi-provisioner"

	// appLabelSelectorKey is common value for 'Key' field in 'LabelSelectorRequirement'
	appLabelSelectorKey = "app"
	// mgrLabelSelector is the key in mgr pod, used for topology spread constraints.
	mgrLabelSelector = "rook-ceph-mgr"
	// monLabelSelector is the key in mon pod, used for topology spread constraints.
	monLabelSelector = "rook-ceph-mon"
	// osdLabelSelector is the key in OSD pod, used for topology spread constraints.
	osdLabelSelector = "rook-ceph-osd"
	// osdPrepareLabelSelector is the key in OSD prepare pod, used for topology spread constraints.
	osdPrepareLabelSelector = "rook-ceph-osd-prepare"
	// mdsLabelSelector is the key in mds pod, used for topology spread constraints.
	mdsLabelSelector = "rook-ceph-mds"
	// rgwLabelSelector is the key in rgw pod, used for topology spread constraints.
	rgwLabelSelector = "rook-ceph-rgw"
	// nfsLabelSelector is the key in nfs pod, used for topology spread constraints.
	nfsLabelSelector = "rook-ceph-nfs"

	// DefaultNodeAffinity is the NodeAffinity to be used when labelSelector is nil
	DefaultNodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: getOcsNodeSelector(),
	}
	// DaemonPlacements map contains the default placement configs for the
	// various OCS daemons
	DaemonPlacements = map[string]rookCephv1.Placement{
		// The empty topology key in TSCs must be replaced with the failure domain key by the caller.
		// This enforces strict even distribution of pods across failure domains.

		"mgr": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, "", "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{mgrLabelSelector}),
			},
		},

		"mon": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, "", "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{monLabelSelector}),
			},
		},

		"osd": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, corev1.LabelHostname, "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{osdLabelSelector}),
			},
		},

		"osd-prepare": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, corev1.LabelHostname, "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{osdLabelSelector, osdPrepareLabelSelector}),
			},
		},

		"rgw": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, "", "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{rgwLabelSelector}),
			},
		},

		"mds": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, "", "ScheduleAnyway",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{mdsLabelSelector}),
			},
		},

		"nfs": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraintWithExpressions(1, "", "DoNotSchedule",
					appLabelSelectorKey, metav1.LabelSelectorOpIn, []string{nfsLabelSelector}),
			},
		},
	}
)

// getTopologySpreadConstraintWithExpressions constructs a TopologySpreadConstraint
// with the specified parameters for label-based topology spreading.
func getTopologySpreadConstraintWithExpressions(
	maxSkew int32, topologyKey string, whenUnsatisfiable corev1.UnsatisfiableConstraintAction,
	labelKey string, labelOperator metav1.LabelSelectorOperator, labelValues []string,
) corev1.TopologySpreadConstraint {
	return corev1.TopologySpreadConstraint{
		MaxSkew:           maxSkew,
		TopologyKey:       topologyKey,
		WhenUnsatisfiable: whenUnsatisfiable,
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      labelKey,
					Operator: labelOperator,
					Values:   labelValues,
				},
			},
		},
	}
}

func GetOcsToleration() corev1.Toleration {
	toleration := corev1.Toleration{
		Key:      NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	return toleration
}

func getOcsNodeSelector() *corev1.NodeSelector {
	nodeSelector := &corev1.NodeSelector{
		NodeSelectorTerms: []corev1.NodeSelectorTerm{
			{
				MatchExpressions: []corev1.NodeSelectorRequirement{
					{
						Key:      NodeAffinityKey,
						Operator: corev1.NodeSelectorOpExists,
					},
				},
			},
		},
	}
	return nodeSelector
}
