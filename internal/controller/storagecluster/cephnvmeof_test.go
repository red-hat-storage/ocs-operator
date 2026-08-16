package storagecluster

import (
	"context"
	"fmt"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// setNVMeOFMetadataPoolReady sets the .nvmeof metadata pool status to Ready,
// simulating Rook having finished creating the pool in RADOS.
func setNVMeOFMetadataPoolReady(t *testing.T, reconciler *StorageClusterReconciler, request reconcile.Request) {
	pool := &cephv1.CephBlockPool{}
	request.Name = nvmeofMetadataPoolName
	err := reconciler.Get(context.TODO(), request.NamespacedName, pool)
	assert.NoError(t, err)

	if pool.Status == nil {
		pool.Status = &cephv1.CephBlockPoolStatus{}
	}
	pool.Status.Phase = cephv1.ConditionReady
	err = reconciler.Status().Update(context.TODO(), pool)
	assert.NoError(t, err)
}

// initStorageClusterResourceCreateUpdateTestWithRequeue is like initStorageClusterResourceCreateUpdateTest
// but expects the reconcile to return a RequeueAfter (e.g. waiting for pool readiness).
func initStorageClusterResourceCreateUpdateTestWithRequeue(t *testing.T, runtimeObjs []client.Object,
	customSpec *api.StorageClusterSpec) (*testing.T, *StorageClusterReconciler, *api.StorageCluster, reconcile.Request) {
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestNoAssert(t, runtimeObjs, customSpec)
	result, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	assert.NotZero(t, result.RequeueAfter, "expected requeue while waiting for metadata pool")
	return t, reconciler, cr, request
}

func TestCephNVMeOF(t *testing.T) {
	var objects []client.Object
	nvmeofSpec := &api.StorageClusterSpec{
		NVMeOF: &api.NVMeOFSpec{
			Enable:           true,
			GatewayInstances: 2,
		},
	}
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithRequeue(t, objects, nvmeofSpec)

	assertNVMeOFBlockPool(t, reconciler, cr, request)
	assertNVMeOFMetadataBlockPool(t, reconciler, cr, request)

	setNVMeOFMetadataPoolReady(t, reconciler, request)
	result, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	assertNVMeOFGateway(t, reconciler, cr, request)
}

func TestCephNVMeOFCustomGroupAndInstances(t *testing.T) {
	var objects []client.Object
	nvmeofSpec := &api.StorageClusterSpec{
		NVMeOF: &api.NVMeOFSpec{
			Enable:           true,
			GatewayGroup:     "group-b",
			GatewayInstances: 4,
		},
	}
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithRequeue(t, objects, nvmeofSpec)

	setNVMeOFMetadataPoolReady(t, reconciler, request)
	result, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	gwName := util.GenerateNameForCephNVMeOFGateway(cr)
	actualGW := &cephv1.CephNVMeOFGateway{}
	request.Name = gwName
	err = reconciler.Get(context.TODO(), request.NamespacedName, actualGW)
	assert.NoError(t, err)
	assert.Equal(t, "group-b", actualGW.Spec.Group)
	assert.Equal(t, 4, actualGW.Spec.Instances)
}

func TestCephNVMeOFDisabled(t *testing.T) {
	var objects []client.Object
	t, reconciler, _, _ := initStorageClusterResourceCreateUpdateTest(t, objects, nil)

	poolName := "ocsinit-nvmeof"
	pool := &cephv1.CephBlockPool{}
	err := reconciler.Get(context.TODO(), types.NamespacedName{Name: poolName}, pool)
	assert.Error(t, err, "NVMeOF data CephBlockPool should not exist when NVMeOF is disabled")

	metadataPool := &cephv1.CephBlockPool{}
	err = reconciler.Get(context.TODO(), types.NamespacedName{Name: "builtin-nvmeof"}, metadataPool)
	assert.Error(t, err, "NVMeOF metadata CephBlockPool should not exist when NVMeOF is disabled")
}


func assertNVMeOFMetadataBlockPool(t *testing.T, reconciler *StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualPool := &cephv1.CephBlockPool{}
	request.Name = "builtin-nvmeof"
	err := reconciler.Get(context.TODO(), request.NamespacedName, actualPool)
	assert.NoError(t, err)

	assert.Equal(t, "builtin-nvmeof", actualPool.Name)
	assert.Equal(t, ".nvmeof", actualPool.Spec.Name)
	assert.Equal(t, getFailureDomain(cr), actualPool.Spec.FailureDomain)
	assert.Equal(t, uint(3), actualPool.Spec.Replicated.Size)
	assert.Equal(t, 1, len(actualPool.OwnerReferences))
}

func assertNVMeOFBlockPool(t *testing.T, reconciler *StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	poolName := util.GenerateNameForNVMeOFBlockPool(cr)
	actualPool := &cephv1.CephBlockPool{}
	request.Name = poolName
	err := reconciler.Get(context.TODO(), request.NamespacedName, actualPool)
	assert.NoError(t, err)

	assert.Equal(t, poolName, actualPool.Name)
	assert.Equal(t, getFailureDomain(cr), actualPool.Spec.FailureDomain)
	assert.Equal(t, uint(3), actualPool.Spec.Replicated.Size)
	assert.Equal(t, 1, len(actualPool.OwnerReferences))
}

func assertNVMeOFGateway(t *testing.T, reconciler *StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	gwName := util.GenerateNameForCephNVMeOFGateway(cr)
	actualGW := &cephv1.CephNVMeOFGateway{}
	request.Name = gwName
	err := reconciler.Get(context.TODO(), request.NamespacedName, actualGW)
	assert.NoError(t, err)

	assert.Equal(t, gwName, actualGW.Name)
	assert.Equal(t, defaultNVMeOFGatewayGroup, actualGW.Spec.Group)
	assert.Equal(t, 2, actualGW.Spec.Instances)
	assert.Equal(t, ptr.To(false), actualGW.Spec.HostNetwork)
	assert.Equal(t, 1, len(actualGW.OwnerReferences))
}

func TestGenerateNVMeOFSubsystemNQN(t *testing.T) {
	result := util.GenerateNVMeOFSubsystemNQN("openshift-storage")
	assert.Equal(t, "nqn.2025-08.io.ceph.rook:openshift-storage", result)
}

func TestGenerateNVMeOFListeners(t *testing.T) {
	sc := createDefaultStorageCluster()
	gwName := util.GenerateNameForCephNVMeOFGateway(sc)
	h := func(suffix string) string {
		return fmt.Sprintf(`{"hostname":"rook-ceph-nvmeof-%s-%s"}`, gwName, suffix)
	}

	tests := []struct {
		name      string
		instances int
		expected  string
	}{
		{
			name:      "single instance",
			instances: 1,
			expected:  fmt.Sprintf("[%s]", h("a")),
		},
		{
			name:      "two instances (default)",
			instances: 2,
			expected:  fmt.Sprintf("[%s,%s]", h("a"), h("b")),
		},
		{
			name:      "three instances",
			instances: 3,
			expected:  fmt.Sprintf("[%s,%s,%s]", h("a"), h("b"), h("c")),
		},
		{
			name:      "four instances",
			instances: 4,
			expected:  fmt.Sprintf("[%s,%s,%s,%s]", h("a"), h("b"), h("c"), h("d")),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := util.GenerateNVMeOFListeners(gwName, tt.instances)
			assert.Equal(t, tt.expected, result)
		})
	}
}
