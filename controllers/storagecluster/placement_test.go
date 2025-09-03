package storagecluster

import (
	"strings"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func getOcsToleration() corev1.Toleration {
	toleration := corev1.Toleration{
		Key:      "node.ocs.openshift.io/storage",
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	return toleration
}

func TestGetPlacement(t *testing.T) {
	// Common storage cluster for all test cases
	baseStorageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
		Status: ocsv1.StorageClusterStatus{
			FailureDomainKey: "topology.kubernetes.io/zone",
		},
	}

	testCases := []struct {
		name      string
		specified rookCephv1.Placement
		component string
		expected  rookCephv1.Placement
	}{
		{
			name:      "default placement for mon component",
			component: "mon",
			expected: rookCephv1.Placement{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-mon"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for arbiter component",
			component: "arbiter",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					defaults.MasterNodeToleration,
				},
			},
		},
		{
			name:      "default placement for osd component",
			component: "osd",
			expected: rookCephv1.Placement{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-osd"},
								},
							},
						},
					},
					{
						MaxSkew:           1,
						TopologyKey:       "kubernetes.io/hostname",
						WhenUnsatisfiable: corev1.ScheduleAnyway,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-osd"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for prepareosd component",
			component: "prepareosd",
			expected: rookCephv1.Placement{
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-osd", "rook-ceph-osd-prepare"},
								},
							},
						},
					},
					{
						MaxSkew:           1,
						TopologyKey:       "kubernetes.io/hostname",
						WhenUnsatisfiable: corev1.ScheduleAnyway,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-osd", "rook-ceph-osd-prepare"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for rgw component with topology key update",
			component: "rgw",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "rook_object_store",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test-cluster-cephobjectstore"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for mds component with topology key update",
			component: "mds",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "rook_file_system",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test-cluster-cephfilesystem"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for nfs component with topology key update",
			component: "nfs",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "ceph_nfs",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test-cluster-cephnfs"},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "default placement for rbd-mirror component",
			component: "rbd-mirror",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name:      "default placement for toolbox component",
			component: "toolbox",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name:      "default placement for noobaa-core component",
			component: "noobaa-core",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},

		{
			name:      "default placement for api-server component",
			component: "api-server",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name:      "default placement for metrics-exporter component",
			component: "metrics-exporter",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name: "mon with custom tolerations",
			specified: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			component: "mon",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "app",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"rook-ceph-mon"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rgw with custom pod affinity",
			specified: rookCephv1.Placement{
				PodAffinity: &corev1.PodAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "app",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"rgw-preferred-app"},
										},
									},
								},
								TopologyKey: "topology.kubernetes.io/zone",
							},
						},
					},
				},
			},
			component: "rgw",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
				PodAffinity: &corev1.PodAffinity{
					PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
						{
							Weight: 100,
							PodAffinityTerm: corev1.PodAffinityTerm{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "app",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"rgw-preferred-app"},
										},
									},
								},
								TopologyKey: "topology.kubernetes.io/zone",
							},
						},
					},
				},
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "rook_object_store",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test-cluster-cephobjectstore"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "mds with custom pod anti affinity",
			specified: rookCephv1.Placement{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-mds"},
									},
								},
							},
							TopologyKey: "topology.kubernetes.io/zone",
						},
					},
				},
			},
			component: "mds",
			expected: rookCephv1.Placement{
				NodeAffinity: defaults.DefaultNodeAffinity,
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-mds"},
									},
								},
							},
							TopologyKey: "topology.kubernetes.io/zone",
						},
					},
				},
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
					{
						MaxSkew:           1,
						TopologyKey:       "topology.kubernetes.io/zone",
						WhenUnsatisfiable: corev1.DoNotSchedule,
						LabelSelector: &metav1.LabelSelector{
							MatchExpressions: []metav1.LabelSelectorRequirement{
								{
									Key:      "rook_file_system",
									Operator: metav1.LabelSelectorOpIn,
									Values:   []string{"test-cluster-cephfilesystem"},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "rbd-mirror with custom pod anti affinity",
			specified: rookCephv1.Placement{
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-rbd-mirror"},
									},
								},
							},
							TopologyKey: "topology.kubernetes.io/zone",
						},
					},
				},
			},
			component: "rbd-mirror",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
				PodAntiAffinity: &corev1.PodAntiAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
						{
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "app",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"rook-ceph-rbd-mirror"},
									},
								},
							},
							TopologyKey: "topology.kubernetes.io/zone",
						},
					},
				},
			},
		},
		{
			name: "toolbox with custom node affinity",
			specified: rookCephv1.Placement{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "custom-toolbox-key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"custom-toolbox-value"},
									},
								},
							},
						},
					},
				},
			},
			component: "toolbox",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "custom-toolbox-key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"custom-toolbox-value"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "noobaa-core with custom tolerations and node affinity",
			specified: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-noobaa-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "custom-noobaa-key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"custom-noobaa-value"},
									},
								},
							},
						},
					},
				},
			},
			component: "noobaa-core",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-noobaa-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "custom-noobaa-key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"custom-noobaa-value"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name: "api-server with custom toleration",
			specified: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-api-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			component: "api-server",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					{
						Key:      "custom-api-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name: "metrics-exporter with custom toleration",
			specified: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
					{
						Key:      "custom-metrics-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
			},
			component: "metrics-exporter",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
					{
						Key:      "custom-metrics-toleration",
						Operator: corev1.TolerationOpExists,
						Effect:   corev1.TaintEffectNoSchedule,
					},
				},
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			name:      "component with label selector",
			component: "all",
			expected: rookCephv1.Placement{
				Tolerations: []corev1.Toleration{
					getOcsToleration(),
				},
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "cluster.ocs.openshift.io/openshift-storage",
										Operator: corev1.NodeSelectorOpExists,
									},
									{
										Key:      "custom-label",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"custom-value"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			name:      "mon with empty placement specification",
			specified: rookCephv1.Placement{},
			component: "mon",
			expected:  rookCephv1.Placement{},
		},
		{
			name:      "osd with empty placement specification",
			specified: rookCephv1.Placement{},
			component: "osd",
			expected:  rookCephv1.Placement{},
		},
		{
			name:      "noobaa-core with empty placement specification",
			specified: rookCephv1.Placement{},
			component: "noobaa-core",
			expected:  rookCephv1.Placement{},
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			sc := baseStorageCluster.DeepCopy()
			// Set the placement specification only if something is specified
			if !isPlacementEmpty(tt.specified) {
				sc.Spec.Placement = map[rookCephv1.KeyType]rookCephv1.Placement{
					rookCephv1.KeyType(tt.component): tt.specified,
				}
			} else if strings.Contains(tt.name, "empty placement specification") {
				// For empty placement tests, explicitly set empty placement
				sc.Spec.Placement = map[rookCephv1.KeyType]rookCephv1.Placement{
					rookCephv1.KeyType(tt.component): tt.specified,
				}
			}
			// Handle special case for label selector test
			if tt.name == "component with label selector" {
				sc.Spec.LabelSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "custom-label",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"custom-value"},
						},
					},
				}
			}
			result := getPlacement(sc, tt.component)
			assert.Equal(t, tt.expected, result)
		})
	}
}
