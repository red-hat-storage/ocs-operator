package storagecluster

import (
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	WorkerAffinityKey = "node-role.kubernetes.io/worker"
	MasterAffinityKey = "node-role.kubernetes.io/master"
)

var masterLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{
		{
			Key:      MasterAffinityKey,
			Operator: metav1.LabelSelectorOpExists,
		},
	},
}
var masterSelectorRequirement = corev1.NodeSelectorRequirement{
	Key:      MasterAffinityKey,
	Operator: corev1.NodeSelectorOpExists,
}

var workerLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{
		{
			Key:      WorkerAffinityKey,
			Operator: metav1.LabelSelectorOpExists,
		},
	},
}
var workerSelectorRequirement = corev1.NodeSelectorRequirement{
	Key:      WorkerAffinityKey,
	Operator: corev1.NodeSelectorOpExists,
}
var workerNodeSelector = corev1.NodeSelector{
	NodeSelectorTerms: []corev1.NodeSelectorTerm{
		{
			MatchExpressions: []corev1.NodeSelectorRequirement{
				workerSelectorRequirement,
			},
		},
	},
}
var workerNodeAffinity = corev1.NodeAffinity{
	RequiredDuringSchedulingIgnoredDuringExecution: &workerNodeSelector,
}
var workerPlacements = map[rookCephv1.KeyType]rookCephv1.Placement{
	"all": {
		NodeAffinity: &workerNodeAffinity,
	},
	"mds": {
		NodeAffinity: &workerNodeAffinity,
	},
	"mon": {
		NodeAffinity: &workerNodeAffinity,
	},
}

var emptyLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{},
}
var emptyPlacements = map[rookCephv1.KeyType]rookCephv1.Placement{
	"all": {},
	"mds": {},
	"mon": {},
}

var customMDSPlacement = rookCephv1.Placement{
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
				TopologyKey: "topology.rook.io/rack",
			},
		},
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "app",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"rook-ceph-mds"},
							},
						},
					},
					TopologyKey: "topology.rook.io/rack",
				},
				Weight: 100,
			},
		},
	},
}

var customMONPlacement = rookCephv1.Placement{
	PodAntiAffinity: &corev1.PodAntiAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
			{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      "app",
							Operator: metav1.LabelSelectorOpIn,
							Values:   []string{"rook-ceph-mon"},
						},
					},
				},
				TopologyKey: "topology.rook.io/rack",
			},
		},
		PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
			{
				PodAffinityTerm: corev1.PodAffinityTerm{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      "app",
								Operator: metav1.LabelSelectorOpIn,
								Values:   []string{"rook-ceph-mon"},
							},
						},
					},
					TopologyKey: "topology.rook.io/rack",
				},
				Weight: 100,
			},
		},
	},
}

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
	cases := []struct {
		label              string
		storageCluster     *ocsv1.StorageCluster
		placements         rookCephv1.PlacementSpec
		labelSelector      *metav1.LabelSelector
		expectedPlacements rookCephv1.PlacementSpec
		failureDomain      string
	}{
		{
			label:          "Case 1: Defaults are preserved i.e no placement and no label selector",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  nil,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
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
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "rook_file_system",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"storage-test-cephfilesystem"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			label:          "Case 2: The configured Placements override the defaults",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  nil,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
				},
				"mon": {},
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
				},
			},
		},
		{
			label:          "Case 3: LabelSelector to modify the default Placements correctly",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  &workerLabelSelector,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity: &workerNodeAffinity,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
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
				"mds": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "rook_file_system",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"storage-test-cephfilesystem"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			label:          "Case 4: LabelSelector modifies an empty NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  &workerLabelSelector,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
				},
				"mon": {
					NodeAffinity: &workerNodeAffinity,
				},
				"mds": {
					NodeAffinity: &workerNodeAffinity,
				},
			},
		},
		{
			label:          "Case 5: LabelSelector modifies a configured NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     workerPlacements,
			labelSelector:  &masterLabelSelector,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										workerSelectorRequirement,
										masterSelectorRequirement,
									},
								},
							},
						},
					},
				},
				"mon": {
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										workerSelectorRequirement,
										masterSelectorRequirement,
									},
								},
							},
						},
					},
				},
				"mds": {
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										workerSelectorRequirement,
										masterSelectorRequirement,
									},
								},
							},
						},
					},
				},
			},
		},
		{
			label:          "Case 6: Empty LabelSelector sets no NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  &emptyLabelSelector,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					Tolerations: defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
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
				"mds": {
					Tolerations: defaults.DaemonPlacements["mds"].Tolerations,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "rook_file_system",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"storage-test-cephfilesystem"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			label:          "Case 7: Custom placement is applied without failure",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"mds": customMDSPlacement,
				"mon": customMONPlacement,
			},
			failureDomain: "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: customMONPlacement.PodAntiAffinity,
				},
				"mds": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: customMDSPlacement.PodAntiAffinity,
				},
			},
		},
		{
			label:          "Case 8: Custom placement is modified by labelSelector",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"mds": customMDSPlacement,
				"mon": customMONPlacement,
			},
			labelSelector: &workerLabelSelector,
			failureDomain: "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: customMONPlacement.PodAntiAffinity,
				},
				"mds": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: customMDSPlacement.PodAntiAffinity,
				},
			},
		},
		{
			label:          "Case 9: FailureDomain zone modifies default placement",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			failureDomain:  "zone",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       corev1.LabelZoneFailureDomainStable,
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "rook_file_system",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"storage-test-cephfilesystem"},
									},
								},
							},
						},
					},
				},
				"mon": {
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       corev1.LabelZoneFailureDomainStable,
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
		},
		{
			label:          "Case 10: Default placement with arbiter enabled",
			storageCluster: mockStorageClusterWithArbiter.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  nil,
			failureDomain:  "rack",
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
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
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
					TopologySpreadConstraints: []corev1.TopologySpreadConstraint{
						{
							MaxSkew:           1,
							TopologyKey:       "topology.rook.io/rack",
							WhenUnsatisfiable: corev1.DoNotSchedule,
							LabelSelector: &metav1.LabelSelector{
								MatchExpressions: []metav1.LabelSelectorRequirement{
									{
										Key:      "rook_file_system",
										Operator: metav1.LabelSelectorOpIn,
										Values:   []string{"storage-test-cephfilesystem"},
									},
								},
							},
						},
					},
				},
			},
		},
		{
			label:          "Case 11: Custom tolerations with zone failure domain",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"all": {
					Tolerations: []corev1.Toleration{
						getOcsToleration(),
					},
				},
				"mon": {
					Tolerations: []corev1.Toleration{
						getOcsToleration(),
					},
				},
				"mds": {
					Tolerations: []corev1.Toleration{
						getOcsToleration(),
					},
				},
			},
			failureDomain: "zone",
			labelSelector: nil,
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					Tolerations: []corev1.Toleration{
						getOcsToleration(),
					},
				},
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
	}

	for _, c := range cases {
		var actualPlacement rookCephv1.Placement
		sc := &ocsv1.StorageCluster{}
		c.storageCluster.DeepCopyInto(sc)
		sc.Spec.Placement = c.placements
		sc.Spec.LabelSelector = c.labelSelector
		if c.failureDomain != "" {
			// Set the FailureDomainKey based on the failure domain
			supportedLabels := map[string]string{
				"rack": "topology.rook.io/rack",
				"host": corev1.LabelHostname,
				"zone": corev1.LabelZoneFailureDomainStable,
			}
			sc.Status.FailureDomainKey = supportedLabels[c.failureDomain]
		}
		expectedPlacement := c.expectedPlacements["all"]
		actualPlacement = getPlacement(sc, "all")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mon"]
		actualPlacement = getPlacement(sc, "mon")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mds"]
		if len(expectedPlacement.TopologySpreadConstraints) > 0 {
			expectedTSCs := make([]corev1.TopologySpreadConstraint, len(expectedPlacement.TopologySpreadConstraints))
			copy(expectedTSCs, expectedPlacement.TopologySpreadConstraints)
			if len(expectedTSCs[0].LabelSelector.MatchExpressions) > 0 {
				expectedTSCs[0].LabelSelector.MatchExpressions[0].Values = []string{util.GenerateNameForCephFilesystem(sc.Name)}
			}
			expectedPlacement.TopologySpreadConstraints = expectedTSCs
		}
		actualPlacement = getPlacement(sc, "mds")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)
	}
}
