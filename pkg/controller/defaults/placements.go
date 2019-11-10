package defaults

import (
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	// DaemonPlacements map contains the default placement configs for the
	// various OCS daemons
	DaemonPlacements = map[string]rook.Placement{
		"all": rook.Placement{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								corev1.NodeSelectorRequirement{
									Key:      NodeAffinityKey,
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				corev1.Toleration{
					Key:      NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},

		"osd": rook.Placement{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								corev1.NodeSelectorRequirement{
									Key:      NodeAffinityKey,
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				corev1.Toleration{
					Key:      NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					corev1.WeightedPodAffinityTerm{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									metav1.LabelSelectorRequirement{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-osd"},
									},
								},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		},

		"rgw": rook.Placement{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								corev1.NodeSelectorRequirement{
									Key:      NodeAffinityKey,
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				corev1.Toleration{
					Key:      NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					corev1.WeightedPodAffinityTerm{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									metav1.LabelSelectorRequirement{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-rgw"},
									},
								},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		},

		"mds": rook.Placement{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								corev1.NodeSelectorRequirement{
									Key:      NodeAffinityKey,
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				corev1.Toleration{
					Key:      NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
			PodAntiAffinity: &corev1.PodAntiAffinity{
				PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
					corev1.WeightedPodAffinityTerm{
						Weight: 100,
						PodAffinityTerm: corev1.PodAffinityTerm{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									metav1.LabelSelectorRequirement{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-mds"},
									},
								},
							},
							TopologyKey: "kubernetes.io/hostname",
						},
					},
				},
			},
		},

		"noobaa-core": rook.Placement{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						corev1.NodeSelectorTerm{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								corev1.NodeSelectorRequirement{
									Key:      NodeAffinityKey,
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			Tolerations: []corev1.Toleration{
				corev1.Toleration{
					Key:      NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}
)
