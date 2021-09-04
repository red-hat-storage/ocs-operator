package storagecluster

import (
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookCore "github.com/rook/rook/pkg/apis/rook.io"

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
var workerPlacements = map[rookCore.KeyType]rookCephv1.Placement{
	"all": {
		NodeAffinity: &workerNodeAffinity,
	},
}

var emptyLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{},
}
var emptyPlacements = map[rookCore.KeyType]rookCephv1.Placement{
	"all": {},
}

var customPlacement = rookCephv1.Placement{
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
		storageCluster     *api.StorageCluster
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
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: defaults.DaemonPlacements["mon"].PodAntiAffinity,
				},
			},
		},
		{
			label:          "Case 2: The configured Placements override the defaults",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  nil,
			expectedPlacements: rookCephv1.PlacementSpec{
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
			},
		},
		{
			label:          "Case 4: LabelSelector modifies an empty NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     emptyPlacements,
			labelSelector:  &workerLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
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
			label:          "Case 5: LabelSelector modifies a configured NodeAffinity",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     workerPlacements,
			labelSelector:  &masterLabelSelector,
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
										// TODO: For this test, this is an expected result that
										// will yield an undesirable CephCluster config. Since
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
			},
		},
		{
			label:          "Case 7: Custom placement is applied without failure",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"mon": customPlacement,
			},
			expectedPlacements: rookCephv1.PlacementSpec{
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
			label:          "Case 8: Custom placement is modified by labelSelector",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements: rookCephv1.PlacementSpec{
				"mon": customPlacement,
			},
			labelSelector: &workerLabelSelector,
			expectedPlacements: rookCephv1.PlacementSpec{
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
			label:          "Case 9: NodeTopologyMap modifies default mon placement",
			storageCluster: mockStorageCluster.DeepCopy(),
			placements:     rookCephv1.PlacementSpec{},
			expectedPlacements: rookCephv1.PlacementSpec{
				"all": {
					NodeAffinity: defaults.DefaultNodeAffinity,
					Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
				},
				"mon": {
					NodeAffinity: defaults.DefaultNodeAffinity,
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
								TopologyKey: zoneTopologyLabel,
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
					NodeAffinity:    defaults.DefaultNodeAffinity,
					PodAntiAffinity: &corev1.PodAntiAffinity{},
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

		expectedPlacement := c.expectedPlacements["all"]
		actualPlacement = getPlacement(sc, "all")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)

		expectedPlacement = c.expectedPlacements["mon"]
		actualPlacement = getPlacement(sc, "mon")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)
	}
}
