package storagecluster

import (
	"context"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestEnsureToolsDeployment(t *testing.T) {
	testcases := []struct {
		label                string
		enableCephTools      bool
		tolerations          []corev1.Toleration
		nodeAffinity         *corev1.NodeAffinity
		expectedTolerations  []corev1.Toleration
		expectedNodeAffinity *corev1.NodeAffinity
	}{
		{
			label:                "Case 1: CephTools enabled with default tolerations and node affinity",
			enableCephTools:      true,
			expectedTolerations:  []corev1.Toleration{getOcsToleration()},
			expectedNodeAffinity: defaults.DefaultNodeAffinity,
		},
		{
			label:           "Case 2: CephTools disabled",
			enableCephTools: false,
		},
		{
			label:           "Case 3: Custom toleration",
			enableCephTools: true,
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expectedTolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expectedNodeAffinity: defaults.DefaultNodeAffinity,
		},
		{
			label:           "Case 4: Custom node affinity",
			enableCephTools: true,
			nodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "test-node-label",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			expectedTolerations: []corev1.Toleration{getOcsToleration()},
			expectedNodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "test-node-label",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools
		if tc.tolerations != nil || tc.nodeAffinity != nil {
			if ocs.Spec.Placement == nil {
				ocs.Spec.Placement = rookCephv1.PlacementSpec{}
			}
			placement := rookCephv1.Placement{}
			if tc.tolerations != nil {
				placement.Tolerations = tc.tolerations
			}
			if tc.nodeAffinity != nil {
				placement.NodeAffinity = tc.nodeAffinity
			}
			ocs.Spec.Placement[rookCephv1.KeyType("toolbox")] = placement
		}
		err := reconciler.ensureToolsDeployment(&ocs)
		assert.NoErrorf(t, err, "[%s] failed to ensure ceph tools deployment", tc.label)
		if tc.enableCephTools {
			cephtoolsDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: request.Namespace}, cephtoolsDeployment)
			assert.NoErrorf(t, err, "[%s] failed to get ceph tools deployment", tc.label)

			assert.Equalf(
				t, tc.expectedTolerations, cephtoolsDeployment.Spec.Template.Spec.Tolerations,
				"[%s]: failed to add toleration to the ceph tool deployment resource", tc.label,
			)
			assert.Equalf(
				t, tc.expectedNodeAffinity, cephtoolsDeployment.Spec.Template.Spec.Affinity.NodeAffinity,
				"[%s]: failed to add node affinity to the ceph tool deployment resource", tc.label,
			)
		}
	}
}

func TestEnsureToolsDeploymentUpdate(t *testing.T) {
	var replicaTwo int32 = 2

	testcases := []struct {
		label                string
		enableCephTools      bool
		tolerations          []corev1.Toleration
		nodeAffinity         *corev1.NodeAffinity
		expectedTolerations  []corev1.Toleration
		expectedNodeAffinity *corev1.NodeAffinity
	}{
		{
			label:                "Case 1: Update existing deployment with default placement",
			enableCephTools:      true,
			expectedTolerations:  []corev1.Toleration{getOcsToleration()},
			expectedNodeAffinity: defaults.DefaultNodeAffinity,
		},
		{
			label:           "Case 2: Delete existing deployment when disabled",
			enableCephTools: false,
		},
		{
			label:           "Case 3: Update with custom toleration and custom node affinity",
			enableCephTools: true,
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			nodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "test-node-label",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
			expectedTolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expectedNodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      "test-node-label",
									Operator: corev1.NodeSelectorOpExists,
								},
							},
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools
		if tc.tolerations != nil || tc.nodeAffinity != nil {
			if ocs.Spec.Placement == nil {
				ocs.Spec.Placement = rookCephv1.PlacementSpec{}
			}
			placement := rookCephv1.Placement{}
			if tc.tolerations != nil {
				placement.Tolerations = tc.tolerations
			}
			if tc.nodeAffinity != nil {
				placement.NodeAffinity = tc.nodeAffinity
			}
			ocs.Spec.Placement[rookCephv1.KeyType("toolbox")] = placement
		}
		cephTools := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rookCephToolDeploymentName,
				Namespace: request.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicaTwo,
			},
		}
		err := reconciler.Client.Create(context.TODO(), cephTools)
		assert.NoError(t, err)
		err = reconciler.ensureToolsDeployment(&ocs)
		assert.NoErrorf(t, err, "[%s] failed to create ceph tools deployment", tc.label)

		cephtoolsDeployment := &appsv1.Deployment{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: request.Namespace}, cephtoolsDeployment)
		if tc.enableCephTools {
			assert.NoErrorf(t, err, "[%s] failed to get ceph tools deployment", tc.label)
			assert.Equalf(t, int32(1), *cephtoolsDeployment.Spec.Replicas, "[%s] failed to update the ceph tools replica count", tc.label)

			assert.Equalf(
				t, tc.expectedTolerations, cephtoolsDeployment.Spec.Template.Spec.Tolerations,
				"[%s]: failed to add toleration to the ceph tool deployment resource", tc.label,
			)
			assert.Equalf(
				t, tc.expectedNodeAffinity, cephtoolsDeployment.Spec.Template.Spec.Affinity.NodeAffinity,
				"[%s]: failed to add node affinity to the ceph tools deployment", tc.label,
			)
		} else {
			assert.Errorf(t, err, "[%s] failed to delete ceph tools deployment when it was disabled in the spec", tc.label)
		}
	}
}

func getTestParams(mockNamespace bool, t *testing.T) (v1.StorageCluster, reconcile.Request, *StorageClusterReconciler) {
	var request reconcile.Request
	if mockNamespace {
		request = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test",
				Namespace: "test-ns",
			},
		}
	} else {
		request = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      request.Name,
				Namespace: request.Namespace,
			},
		}
	}
	ocs := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Name,
			Namespace: request.Namespace,
		},
	}

	reconciler := getTheReconciler(t, &ocs)
	//The fake client stores the objects after adding a resource version to
	//them. This is a breaking change introduced in
	//https://github.com/kubernetes-sigs/controller-runtime/pull/1306.
	//Therefore we cannot use the fake object that we provided as input to the
	//the fake client and should use the object obtained from the Get
	//operation.
	_ = reconciler.Client.Get(context.TODO(), request.NamespacedName, &ocs)

	return ocs, request, reconciler
}

func getTheReconciler(t *testing.T, objs ...runtime.Object) *StorageClusterReconciler {
	sc := &v1.StorageCluster{}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).WithStatusSubresource(sc).Build()

	return &StorageClusterReconciler{
		Scheme: scheme,
		Client: client,
	}
}
