package storagecluster

import (
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func TestCephBlockPools(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, nil)
		assertCephBlockPools(t, reconciler, cr, request)
	}
}

func assertCephBlockPools(t *testing.T, reconciler ReconcileStorageCluster, cr *api.StorageCluster, request reconcile.Request) {
	actualCbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	request.Name = "ocsinit-cephblockpool"
	err := reconciler.client.Get(nil, request.NamespacedName, actualCbp)
	assert.NoError(t, err)

	expectedCbp, err := reconciler.newCephBlockPoolInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedCbp[0].OwnerReferences), 1)

	assert.Equal(t, expectedCbp[0].ObjectMeta.Name, actualCbp.ObjectMeta.Name)
	assert.Equal(t, expectedCbp[0].Spec, actualCbp.Spec)
}
