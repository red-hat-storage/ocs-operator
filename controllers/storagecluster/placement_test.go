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

// getCustomInfraToleration mirrors a user-supplied toleration (e.g. for infra node taints) that is
// distinct from the operator's default OCS toleration. Used to exercise the additive merge.
func getCustomInfraToleration() corev1.Toleration {
	return corev1.Toleration{
		Key:      "node-role.kubernetes.io/infra",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoSchedule,
	}
}

func TestGetPlacement(t *testing.T) {
	cases := []struct {
		label              string
		storageCluster     *ocsv1.StorageCluster
		placements         rookCephv1.PlacementSpec
		labelSelector      *metav1.LabelSelector
		expectedPlacements rookCephv1.PlacementSpec
		topologyMap        *ocsv1.NodeTopologyMap
	}{
		{
			label:          "Case 1: Defaults are preserved i.e no placement and no label selector",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  nil,
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
				"mds": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mds"].PodAntiAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 2: The configured Placements override the defaults",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  nil,
			expectedPlacements: rookCephv1.PlacementSpec{
				// default tolerations are additively merged into the specified placement
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {},
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 3: LabelSelector to modify the default Placements correctly",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  &workerLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
				"mds": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mds"].PodAntiAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 4: LabelSelector modifies an empty NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  &workerLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
				// default tolerations are additively merged into the specified placement
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity: &workerNodeAffinity,
				},
				"mds": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 5: LabelSelector modifies a configured NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     workerPlacements,
			labelSelector:  &masterLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
				// default tolerations are additively merged into the specified placement
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
					Tolerations: defaults.DaemonPlacements["all"].Tolerations,
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
					Tolerations: defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 6: Empty LabelSelector sets no NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  &emptyLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					Tolerations: defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
				"mds": {
					PodAntiAffinity: defaults.DaemonPlacements["mds"].PodAntiAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
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
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: customMONPlacement.PodAntiAffinity,
				},
				// default mds tolerations are additively merged into the custom mds placement
				"mds": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: customMDSPlacement.PodAntiAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
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
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: customMONPlacement.PodAntiAffinity,
				},
				// default mds tolerations are additively merged into the custom mds placement
				"mds": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: customMDSPlacement.PodAntiAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
				},
			},
		},
		{
			label:          "Case 9: NodeTopologyMap modifies default mon placement",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["mds"].Tolerations,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
							{
								LabelSelector: &metav1.LabelSelector{
									MatchExpressions: []metav1.LabelSelectorRequirement{
										{
											Key:      "rook_file_system",
											Operator: metav1.LabelSelectorOpIn,
											Values:   []string{"storage-test-cephfilesystem"},
										},
									},
								},
								TopologyKey: corev1.LabelZoneFailureDomainStable,
							},
						},
					},
				},

				"mon": {
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
								TopologyKey: corev1.LabelZoneFailureDomainStable,
							},
						},
					},
				},
			},
			topologyMap: &ocsv1.NodeTopologyMap{
				Labels: map[string]ocsv1.TopologyLabelValues{
					corev1.LabelZoneFailureDomainStable: []string{
						"zone1",
						"zone2",
						"zone3",
					},
				},
			},
		},
		{
			label:          "Case 10: skip podAntiAffinity in mon placement in case of stretched cluster",
			storageCluster: mockStorageClusterWithArbiter.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			labelSelector:  nil,
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: &corev1.PodAntiAffinity{},
				},
				"mds": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					Tolerations:     defaults.DaemonPlacements["all"].Tolerations,
					PodAntiAffinity: defaults.DaemonPlacements["mds"].PodAntiAffinity,
				},
			},
		},
		{
			label:          "Case 11: When PodAntiAffinity is empty and topologyMap is provided ",
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
			topologyMap: &ocsv1.NodeTopologyMap{
				Labels: map[string]ocsv1.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
						"zone3",
					},
				},
			},
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
					PodAntiAffinity: nil,
				},
				"mds": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					Tolerations:     defaults.DaemonPlacements["mds"].Tolerations,
					PodAntiAffinity: nil,
				},
			},
		},
		{
			// DFBUGS-5253: a user-supplied custom toleration must be merged with (not replace)
			// the operator's default tolerations. This is the core additive-merge behavior.
			label:          "Case 12: Custom toleration is additively merged with the default toleration",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"all": {
					Tolerations: []corev1.Toleration{getCustomInfraToleration()},
				},
				"mon": {
					Tolerations: []corev1.Toleration{getCustomInfraToleration()},
				},
				"mds": {
					Tolerations: []corev1.Toleration{getCustomInfraToleration()},
				},
			},
			labelSelector: nil,
			expectedPlacements: rookCephv1.PlacementSpec{
				// "all" has a default OCS toleration, so the result is default + custom
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  []corev1.Toleration{getOcsToleration(), getCustomInfraToleration()},
				},
				// "mon" has no default toleration of its own (it inherits the OCS toleration via the
				// "all" key at the Rook level), so only the custom toleration is present here
				"mon": {
					Tolerations: []corev1.Toleration{getCustomInfraToleration()},
				},
				// "mds" has a default OCS toleration, so the result is default + custom
				"mds": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  []corev1.Toleration{getOcsToleration(), getCustomInfraToleration()},
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
		sc.Status.NodeTopologies = c.topologyMap
		if c.topologyMap != nil {
			setFailureDomain(sc)
		}
		expectedPlacement := c.expectedPlacements["all"]
		actualPlacement = getPlacement(sc, "all")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mon"]
		actualPlacement = getPlacement(sc, "mon")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mds"]
		testPodAffinity := &corev1.PodAntiAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
				defaults.GetMdsWeightedPodAffinityTerm(100, util.GenerateNameForCephFilesystem(sc.Name)).PodAffinityTerm,
			},
		}
		if expectedPlacement.PodAntiAffinity != nil {
			topologyKeys := ""
			if len(expectedPlacement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution) != 0 {
				topologyKeys = expectedPlacement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].TopologyKey
			}
			expectedPlacement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution = testPodAffinity.RequiredDuringSchedulingIgnoredDuringExecution
			if topologyKeys != "" {
				expectedPlacement.PodAntiAffinity.RequiredDuringSchedulingIgnoredDuringExecution[0].TopologyKey = topologyKeys
			}
		}
		actualPlacement = getPlacement(sc, "mds")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)
	}
}

func TestAppendUniqueTolerations(t *testing.T) {
	tol1 := corev1.Toleration{
		Key:      "key1",
		Operator: corev1.TolerationOpEqual,
		Value:    "value1",
		Effect:   corev1.TaintEffectNoSchedule,
	}
	tol2 := corev1.Toleration{
		Key:      "key2",
		Operator: corev1.TolerationOpExists,
		Effect:   corev1.TaintEffectNoExecute,
	}

	testCases := []struct {
		name       string
		base       []corev1.Toleration
		additional []corev1.Toleration
		expected   []corev1.Toleration
	}{
		{
			name:       "empty base",
			base:       nil,
			additional: []corev1.Toleration{tol1},
			expected:   []corev1.Toleration{tol1},
		},
		{
			name:       "empty additional",
			base:       []corev1.Toleration{tol1},
			additional: nil,
			expected:   []corev1.Toleration{tol1},
		},
		{
			name:       "no duplicates",
			base:       []corev1.Toleration{tol1},
			additional: []corev1.Toleration{tol2},
			expected:   []corev1.Toleration{tol1, tol2},
		},
		{
			name:       "with duplicates",
			base:       []corev1.Toleration{tol1, tol2},
			additional: []corev1.Toleration{tol1}, // duplicate
			expected:   []corev1.Toleration{tol1, tol2},
		},
		{
			name:       "all duplicates",
			base:       []corev1.Toleration{tol1},
			additional: []corev1.Toleration{tol1},
			expected:   []corev1.Toleration{tol1},
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			result := appendUniqueTolerations(tt.base, tt.additional)
			assert.Equal(t, tt.expected, result)
		})
	}
}
