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

var mockPlacements = map[rookv1.KeyType]rookv1.Placement{
	"all": rookv1.Placement{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							corev1.NodeSelectorRequirement{
								Key:      WorkerAffinityKey,
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			},
		},
		Tolerations: []corev1.Toleration{
			corev1.Toleration{
				Key:      defaults.NodeTolerationKey,
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		},
	},
}
var defaultLabelPlacement = map[rookv1.KeyType]rookv1.Placement{
	"all": rookv1.Placement{
		NodeAffinity: &corev1.NodeAffinity{
			RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
				NodeSelectorTerms: []corev1.NodeSelectorTerm{
					corev1.NodeSelectorTerm{
						MatchExpressions: []corev1.NodeSelectorRequirement{
							corev1.NodeSelectorRequirement{
								Key:      defaults.NodeAffinityKey,
								Operator: corev1.NodeSelectorOpExists,
							},
						},
					},
				},
			},
		},
		Tolerations: []corev1.Toleration{
			corev1.Toleration{
				Key:      defaults.NodeTolerationKey,
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			},
		},
	},
}

func TestGetPlacement(t *testing.T) {
	// Case 1: Defaults are preserved i.e no placement and no label selector
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	assert.Equal(t, defaultLabelPlacement["all"], getPlacement(sc, "all"))

	// Case 2: The configured Placements override the defaults
	sc = &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.Placement = mockPlacements
	assert.Equal(t, defaultLabelPlacement["all"], getPlacement(sc, "all"))

	// Case 3: LabelSelector to modify the default placements correctly
	sc = &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.LabelSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			metav1.LabelSelectorRequirement{
				Key:      WorkerAffinityKey,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	assert.Equal(t, mockPlacements["all"], getPlacement(sc, "all"))

	// Case 4: The LabelSelector modifies the configured Placements correctly
	sc = &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.Placement = mockPlacements
	sc.Spec.LabelSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			metav1.LabelSelectorRequirement{
				Key:      MasterAffinityKey,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	expectedPlacements := mockPlacements
	expectedPlacements["all"].NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions[0].Key = MasterAffinityKey
	assert.Equal(t, expectedPlacements["all"], getPlacement(sc, "all"))

	// Case 5: Empty LabelSelector sets no Node Affinity
	sc = &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.LabelSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{},
	}
	expectedPlacement := defaults.DaemonPlacements["all"]
	expectedPlacement.NodeAffinity = &corev1.NodeAffinity{
		RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{},
	}
	assert.Equal(t, expectedPlacement, getPlacement(sc, "all"))
}
