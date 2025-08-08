package defaults

import (
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	APIServerKey       = "api-server"
	MetricsExporterKey = "metrics-exporter"

	monLableSelector = "rook-ceph-mon"
	// osdLabelSelector is the key in OSD pod. Used
	// as a label selector for topology spread constraints.
	osdLabelSelector = "rook-ceph-osd"
	// osdPrepareLabelSelector is the key in OSD prepare pod. Used
	// as a label selector for topology spread constraints.
	osdPrepareLabelSelector = "rook-ceph-osd-prepare"
	// appLabelSelectorKey is common value for 'Key' field in 'LabelSelectorRequirement'
	appLabelSelectorKey = "app"
	// DefaultNodeAffinity is the NodeAffinity to be used when labelSelector is nil
	DefaultNodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: getOcsNodeSelector(),
	}
	// DaemonPlacements map contains the default placement configs for the
	// various OCS daemons
	DaemonPlacements = map[string]rookCephv1.Placement{
		"all": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		"mon": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraint(1, appLabelSelectorKey, []string{monLableSelector}, corev1.DoNotSchedule),
			},
		},

		"osd": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraint(1, appLabelSelectorKey, []string{osdLabelSelector}, corev1.ScheduleAnyway),
			},
		},

		"osd-prepare": {
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				getTopologySpreadConstraint(1, appLabelSelectorKey, []string{osdLabelSelector, osdPrepareLabelSelector}, corev1.ScheduleAnyway),
			},
		},
		"rgw": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				// leave the label value empty as it will be updated with the objectStore name later in getPlacement()
				getTopologySpreadConstraint(1, "rook_object_store", []string{}, corev1.DoNotSchedule),
			},
		},

		"mds": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				// leave the label value empty as it will be updated with the filesystem name later in getPlacement()
				getTopologySpreadConstraint(1, "rook_file_system", []string{}, corev1.DoNotSchedule),
			},
		},

		"nfs": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				// leave the label value empty as it will be updated with the nfs name later in getPlacement()
				getTopologySpreadConstraint(1, "ceph_nfs", []string{}, corev1.DoNotSchedule),
			},
		},

		"noobaa-core": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		"noobaa-standalone": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		"rbd-mirror": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		APIServerKey: {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		MetricsExporterKey: {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},
	}
)

// getTopologySpreadConstraint populates values required & returns the topology spread constraint
func getTopologySpreadConstraint(maxSkew int32, labelKey string, labelValues []string, whenUnsatisfiable corev1.UnsatisfiableConstraintAction) corev1.TopologySpreadConstraint {
	topologySpreadConstraint := corev1.TopologySpreadConstraint{
		MaxSkew:           maxSkew,
		TopologyKey:       corev1.LabelHostname, // TopologyKey gets updated with the failure domain in newStorageClassDeviceSets()/getPlacement()
		WhenUnsatisfiable: whenUnsatisfiable,
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      labelKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   labelValues,
				},
			},
		},
	}
	return topologySpreadConstraint
}

func getOcsToleration() corev1.Toleration {
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
