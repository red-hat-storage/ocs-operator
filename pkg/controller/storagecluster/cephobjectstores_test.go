package storagecluster

import (
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func TestCephObjectStores(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, nil)
		assertCephObjectStores(t, reconciler, cr, request)
		t, reconciler, cr, request = initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, createUpdateRuntimeObjects(cp))
		assertCephObjectStores(t, reconciler, cr, request)
	}
}
func assertCephObjectStores(t *testing.T, reconciler ReconcileStorageCluster, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephObjectStoreInstances(cr)
	assert.NoError(t, err)

	actualCos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstore",
		},
	}
	request.Name = "ocsinit-cephobjectstore"
	err = reconciler.client.Get(nil, request.NamespacedName, actualCos)
	// for any cloud platform, 'cephobjectstore' should not be created
	// 'Get' should have thrown an error
	if isValidCloudPlatform(reconciler.platform.platform) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCos[0].ObjectMeta.Name, actualCos.ObjectMeta.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Condition(
			t, func() bool { return expectedCos[0].Spec.Gateway.Instances > 1 },
			"there should be multiple 'Spec.Gateway.Instances'")
		assert.Equal(
			t, expectedCos[0].Spec.Gateway.Placement, defaults.DaemonPlacements["rgw"])
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)
}
