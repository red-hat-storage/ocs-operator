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
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					getPodAffinityTerm("rook-ceph-mon"),
				},
			},
		},

		"osd": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			// The first TSC is a hard constraint which restricts the movement of OSDs between failure domain
			// The second TSC is a soft constraint which restricts the movement of OSDs between hosts
			// The topology key in the first TSC is set to empty string, it should be updated to the failure domain key
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "",
					WhenUnsatisfiable: "DoNotSchedule",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							appLabelSelectorKey: osdLabelSelector,
						},
					},
				},
				{
					MaxSkew:           1,
					TopologyKey:       corev1.LabelHostname,
					WhenUnsatisfiable: "ScheduleAnyway",
					LabelSelector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							appLabelSelectorKey: osdLabelSelector,
						},
					},
				},
			},
		},

		"osd-prepare": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			// The first TSC is a hard constraint which restricts the movement of OSD-prepares between failure domain
			// The second TSC is a soft constraint which restricts the movement of OSD-prepares between hosts
			// The topology key in the first TSC is set to empty string, it should be updated to the failure domain key
			// The TSCs for prepare pods should take into account both the osdLabelSelector and osdPrepareLabelSelector
			// This is due to the fact that some of the prepare job pods might have been removed after completion
			TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
				{
					MaxSkew:           1,
					TopologyKey:       "",
					WhenUnsatisfiable: "DoNotSchedule",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      appLabelSelectorKey,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{osdLabelSelector, osdPrepareLabelSelector},
							},
						},
					},
				},
				{
					MaxSkew:           1,
					TopologyKey:       corev1.LabelHostname,
					WhenUnsatisfiable: "ScheduleAnyway",
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      appLabelSelectorKey,
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{osdLabelSelector, osdPrepareLabelSelector},
							},
						},
					},
				},
			},
		},

		"rgw": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					getWeightedPodAffinityTerm(100, "rook-ceph-rgw"),
				},
			},
		},

		"mds": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					getWeightedPodAffinityTerm(100, "rook-ceph-mds"),
				},
			},
		},

		"nfs": {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
					getPodAffinityTerm("rook-ceph-nfs"),
				},
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

		CsiPluginKey: {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},

		CsiProvisionerKey: {
			Tolerations: []corev1.Toleration{
				getOcsToleration(),
			},
		},
	}
)

func getWeightedPodAffinityTerm(weight int32, selectorValue ...string) corev1.WeightedPodAffinityTerm {
	WeightedPodAffinityTerm := corev1.WeightedPodAffinityTerm{
		Weight: weight,
		PodAffinityTerm: corev1.PodAffinityTerm{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      appLabelSelectorKey,
						Operator: metav1.LabelSelectorOpIn,
						Values:   selectorValue,
					},
				},
			},
			TopologyKey: corev1.LabelHostname,
		},
	}
	return WeightedPodAffinityTerm
}

func getPodAffinityTerm(selectorValue ...string) corev1.PodAffinityTerm {
	podAffinityTerm := corev1.PodAffinityTerm{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      appLabelSelectorKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   selectorValue,
				},
			},
		},
		TopologyKey: corev1.LabelHostname,
	}
	return podAffinityTerm
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
