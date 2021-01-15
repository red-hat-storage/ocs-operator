package storagecluster

import (
	"testing"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	rookv1 "github.com/rook/rook/pkg/apis/rook.io/v1"
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
var workerPlacements = map[rookv1.KeyType]rookv1.Placement{
	"all": {
		NodeAffinity: &workerNodeAffinity,
	},
}

var emptyLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{},
}
var emptyPlacements = map[rookv1.KeyType]rookv1.Placement{
	"all": {},
}

var customPlacement = rookv1.Placement{
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

func TestGetPlacement(t *testing.T) {
	cases := []struct {
		label              string
		placements         rookv1.PlacementSpec
		labelSelector      *metav1.LabelSelector
		expectedPlacements rookv1.PlacementSpec
		topologyMap        *ocsv1.NodeTopologyMap
	}{
		{
			label:         "Test Case 1: Defaults are preserved i.e no placement and no label selector",
			placements:    rookv1.PlacementSpec{},
			labelSelector: nil,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:         "Case 2: The configured Placements override the defaults",
			placements:    emptyPlacements,
			labelSelector: nil,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
				},
				"mon": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:         "Case 3: LabelSelector to modify the default Placements correctly",
			placements:    rookv1.PlacementSpec{},
			labelSelector: &workerLabelSelector,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:         "Case 4: LabelSelector modifies an empty NodeAffinity",
			placements:    emptyPlacements,
			labelSelector: &workerLabelSelector,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:         "Case 5: LabelSelector modifies a configured NodeAffinity",
			placements:    workerPlacements,
			labelSelector: &masterLabelSelector,
			expectedPlacements: rookv1.PlacementSpec{
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
										// TODO: For this test, this is an expected result that
										// will yield an undesireable CephCluster config. Since
										// NodeAffinity will be defined in the CephCluster, the
										// "all" NodeAffinity will not apply, meaning mons will
										// only run on master nodes. We should figure out a way
										// to prevent this, if possible.
										masterSelectorRequirement,
									},
								},
							},
						},
					},
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:         "Case 6: Empty LabelSelector sets no NodeAffinity",
			placements:    rookv1.PlacementSpec{},
			labelSelector: &emptyLabelSelector,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					Tolerations: defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label: "Case 7: Custom placement is applied without failure",
			placements: rookv1.PlacementSpec{
				"mon": customPlacement,
			},
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: customPlacement.PodAntiAffinity,
				},
			},
		},
		{
			label: "Case 8: Custom placement is modified by labelSelector",
			placements: rookv1.PlacementSpec{
				"mon": customPlacement,
			},
			labelSelector: &workerLabelSelector,
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: &workerNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity:    &workerNodeAffinity,
					PodAntiAffinity: customPlacement.PodAntiAffinity,
				},
			},
		},
		{
			label:      "Case 9: NodeTopologyMap modifies default mon placement",
			placements: rookv1.PlacementSpec{},
			expectedPlacements: rookv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					PodAntiAffinity: &corev1.PodAntiAffinity{
						PreferredDuringSchedulingIgnoredDuringExecution: []corev1.WeightedPodAffinityTerm{
							{
								Weight: 100,
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
									TopologyKey: zoneTopologyLabel,
								},
							},
						},
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
		},
	}

	for _, c := range cases {
		var actualPlacement rookv1.Placement
		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Placement = c.placements
		sc.Spec.LabelSelector = c.labelSelector
		sc.Status.NodeTopologies = c.topologyMap

		expectedPlacement := c.expectedPlacements["all"]
		actualPlacement = getPlacement(sc, "all")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mon"]
		actualPlacement = getPlacement(sc, "mon")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)
	}
}
