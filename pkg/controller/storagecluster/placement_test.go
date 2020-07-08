package storagecluster

import (
	"testing"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
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
		metav1.LabelSelectorRequirement{
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
		metav1.LabelSelectorRequirement{
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
		corev1.NodeSelectorTerm{
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
	"all": rookv1.Placement{
		NodeAffinity: &workerNodeAffinity,
	},
}

var emptyLabelSelector = metav1.LabelSelector{
	MatchExpressions: []metav1.LabelSelectorRequirement{},
}
var emptyPlacements = map[rookv1.KeyType]rookv1.Placement{
	"all": rookv1.Placement{},
}

func TestGetPlacement(t *testing.T) {
	cases := []struct {
		label             string
		placements        rookv1.PlacementSpec
		labelSelector     *metav1.LabelSelector
		expectedPlacement rookv1.Placement
	}{
		{
			label:         "Test Case 1: Defaults are preserved i.e no placement and no label selector",
			placements:    rookv1.PlacementSpec{},
			labelSelector: nil,
			expectedPlacement: rookv1.Placement{
				NodeAffinity: defaults.DefaultNodeAffinity,
				Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
			},
		},
		{
			label:         "Case 2: The configured Placements override the defaults",
			placements:    emptyPlacements,
			labelSelector: nil,
			expectedPlacement: rookv1.Placement{
				NodeAffinity: defaults.DefaultNodeAffinity,
			},
		},
		{
			label:         "Case 3: LabelSelector to modify the default Placements correctly",
			placements:    rookv1.PlacementSpec{},
			labelSelector: &workerLabelSelector,
			expectedPlacement: rookv1.Placement{
				NodeAffinity: &workerNodeAffinity,
				Tolerations:  defaults.DaemonPlacements["all"].Tolerations,
			},
		},
		{
			label:         "Case 4: LabelSelector modifies an empty NodeAffinity",
			placements:    emptyPlacements,
			labelSelector: &workerLabelSelector,
			expectedPlacement: rookv1.Placement{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &workerNodeSelector,
				},
			},
		},
		{
			label:         "Case 5: LabelSelector modifies a configured NodeAffinity",
			placements:    workerPlacements,
			labelSelector: &masterLabelSelector,
			expectedPlacement: rookv1.Placement{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							corev1.NodeSelectorTerm{
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
		{
			label:         "Case 6: Empty LabelSelector sets no NodeAffinity",
			placements:    rookv1.PlacementSpec{},
			labelSelector: &emptyLabelSelector,
			expectedPlacement: rookv1.Placement{
				Tolerations: defaults.DaemonPlacements["all"].Tolerations,
			},
		},
	}

	for _, c := range cases {
		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Placement = c.placements
		sc.Spec.LabelSelector = c.labelSelector
		expectedPlacement := c.expectedPlacement
		actualPlacement := getPlacement(sc, "all")
		assert.Equal(t, expectedPlacement, actualPlacement, c.label)
	}
}
