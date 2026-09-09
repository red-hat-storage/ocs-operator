package util

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func TestKernelVersionEndToEnd(t *testing.T) {

	ctx := context.TODO()

	t.Run("supported kernel versions", func(t *testing.T) {

		nodes := []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KernelVersion: "5.14.0-570.22.1.el9_3.x86_64"}},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node2"},
				Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KernelVersion: "5.14.0-572.23.1.el9_4.x86_64"}},
			},
		}

		var objs []client.Object
		for i := range nodes {
			objs = append(objs, &nodes[i])
		}
		cli := fake.NewClientBuilder().WithObjects(objs...).Build()

		versions, err := GetKernelVersionFromAllNodes(ctx, cli)
		assert.NoError(t, err)
		assert.Len(t, versions, 2)

		supported, err := AreKernelVersionsSupported(versions, CephxKeyRotaionKernelSupportVersion)
		assert.NoError(t, err)
		assert.True(t, supported)
	})

	t.Run("unsupported kernel versions", func(t *testing.T) {

		nodes := []corev1.Node{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node1"},
				Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KernelVersion: "5.14.0-570.21.1.el9_3.x86_64"}}, // lower than supported
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "node2"},
				Status:     corev1.NodeStatus{NodeInfo: corev1.NodeSystemInfo{KernelVersion: "5.14.0-572.23.1.el9_4.x86_64"}},
			},
		}

		var objs []client.Object
		for i := range nodes {
			objs = append(objs, &nodes[i])
		}
		cli := fake.NewClientBuilder().WithObjects(objs...).Build()

		versions, err := GetKernelVersionFromAllNodes(ctx, cli)
		assert.NoError(t, err)
		assert.Len(t, versions, 2)

		supported, err := AreKernelVersionsSupported(versions, CephxKeyRotaionKernelSupportVersion)
		assert.NoError(t, err)
		assert.False(t, supported)
	})

}
