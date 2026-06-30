package storagecluster

import (
	"context"
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

func TestCephNVMeOF(t *testing.T) {
	var objects []client.Object
	nvmeofSpec := &api.StorageClusterSpec{
		NVMeOF: &api.NVMeOFSpec{},
	}
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nvmeofSpec)

	assertNVMeOFBlockPool(t, reconciler, cr, request)
	assertNVMeOFGateway(t, reconciler, cr, request)
}

func TestCephNVMeOFCustomGroupAndInstances(t *testing.T) {
	var objects []client.Object
	nvmeofSpec := &api.StorageClusterSpec{
		NVMeOF: &api.NVMeOFSpec{
			GatewayGroup:     "group-b",
			GatewayInstances: 4,
		},
	}
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nvmeofSpec)

	gwName := util.GenerateNameForCephNVMeOFGateway(cr)
	actualGW := &cephv1.CephNVMeOFGateway{}
	request.Name = gwName
	err := reconciler.Get(context.TODO(), request.NamespacedName, actualGW)
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
	assert.Error(t, err, "NVMeOF CephBlockPool should not exist when NVMeOF is disabled")
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
	assert.Equal(t, util.GenerateNameForNVMeOFBlockPool(cr), actualGW.Spec.Pool)
	assert.Equal(t, defaultNVMeOFGatewayGroup, actualGW.Spec.Group)
	assert.Equal(t, defaultNVMeOFGatewayInstances, actualGW.Spec.Instances)
	assert.Equal(t, ptr.To(false), actualGW.Spec.HostNetwork)
	assert.Equal(t, 1, len(actualGW.OwnerReferences))
}
