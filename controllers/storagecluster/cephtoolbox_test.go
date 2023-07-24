package storagecluster

import (
	"context"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
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
		label           string
		enableCephTools bool
		tolerations     []corev1.Toleration
	}{
		{
			label:           "Case 1",
			enableCephTools: true,
			tolerations:     []corev1.Toleration{},
		},
		{
			label:           "Case 2",
			enableCephTools: false,
			tolerations:     []corev1.Toleration{},
		},
		{
			label:           "Case 3",
			enableCephTools: true,
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
	}

	defaultTolerations := []corev1.Toleration{{
		Key:      defaults.NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools
		if ocs.Spec.Placement == nil {
			ocs.Spec.Placement = rookCephv1.PlacementSpec{}
		}
		ocs.Spec.Placement[rookCephv1.KeyType("toolbox")] = rookCephv1.Placement{
			Tolerations: tc.tolerations,
		}
		err := reconciler.ensureToolsDeployment(&ocs)
		assert.NoErrorf(t, err, "[%s] failed to create ceph tools", tc.label)
		if tc.enableCephTools {
			cephtoolsDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: request.Namespace}, cephtoolsDeployment)
			assert.NoErrorf(t, err, "[%s] failed to create ceph tools", tc.label)

			assert.Equalf(
				t, cephtoolsDeployment.Spec.Template.Spec.Tolerations, append(defaultTolerations, tc.tolerations...),
				"[%s]: failed to add toleration to the ceph tool deployment resource", tc.label,
			)
		}
	}
}

func TestEnsureToolsDeploymentUpdate(t *testing.T) {
	var replicaTwo int32 = 2

	testcases := []struct {
		label           string
		enableCephTools bool
		tolerations     []corev1.Toleration
	}{
		{
			label:           "Case 1", // existing ceph tools pod should get updated
			enableCephTools: true,
			tolerations:     []corev1.Toleration{},
		},
		{
			label:           "Case 2", // existing ceph tool pod should get deleted
			enableCephTools: false,
		},
		{
			label:           "Case 3",
			enableCephTools: true,
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
	}

	defaultTolerations := []corev1.Toleration{{
		Key:      defaults.NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools
		if ocs.Spec.Placement == nil {
			ocs.Spec.Placement = rookCephv1.PlacementSpec{}
		}
		ocs.Spec.Placement[rookCephv1.KeyType("toolbox")] = rookCephv1.Placement{
			Tolerations: tc.tolerations,
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
			assert.Equalf(t, int32(1), *cephtoolsDeployment.Spec.Replicas, "[%s] failed to update the ceph tools pod", tc.label)

			assert.Equalf(
				t, cephtoolsDeployment.Spec.Template.Spec.Tolerations, append(defaultTolerations, tc.tolerations...),
				"[%s]: failed to add toleration to the ceph tool deployment resource", tc.label,
			)

		} else {
			assert.Errorf(t, err, "[%s] failed to delete ceph tools deployment when it was disabled in the spec", tc.label)
		}
	}
}

func TestRemoveDuplicateTolerations(t *testing.T) {
	testcases := []struct {
		label       string
		tolerations []corev1.Toleration
		expected    []corev1.Toleration
	}{
		{
			label: "Case 1",
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expected: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
		{
			label: "Case 2",
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}, {
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expected: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
		{
			label: "Case 3",
			tolerations: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}, {
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}, {
				Key:      "test-toleration-2",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
			expected: []corev1.Toleration{{
				Key:      "test-toleration",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}, {
				Key:      "test-toleration-2",
				Operator: corev1.TolerationOpEqual,
				Value:    "true",
				Effect:   corev1.TaintEffectNoSchedule,
			}},
		},
	}

	for _, tc := range testcases {
		actual := removeDuplicateTolerations(tc.tolerations)
		assert.Equalf(t, tc.expected, actual, "[%s] failed to remove duplicate tolerations", tc.label)
	}
}

func getTestParams(mockNamespace bool, t *testing.T) (v1.StorageCluster, reconcile.Request, StorageClusterReconciler) {
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

func getTheReconciler(t *testing.T, objs ...runtime.Object) StorageClusterReconciler {
	sc := &v1.StorageCluster{}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(objs...).WithStatusSubresource(sc).Build()

	return StorageClusterReconciler{
		Scheme:   scheme,
		Client:   client,
		platform: &Platform{},
	}
}
