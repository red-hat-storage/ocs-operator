package storagecluster

import (
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func TestCephFileSystem(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, nil)
		assertCephFileSystem(t, reconciler, cr, request)
		t, reconciler, cr, request = initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, createUpdateRuntimeObjects(cp))
		assertCephFileSystem(t, reconciler, cr, request)
	}

}

func assertCephFileSystem(t *testing.T, reconciler ReconcileStorageCluster, cr *api.StorageCluster, request reconcile.Request) {
	actualFs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
	}
	request.Name = "ocsinit-cephfilesystem"
	err := reconciler.client.Get(nil, request.NamespacedName, actualFs)
	assert.NoError(t, err)

	expectedAf, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedAf[0].OwnerReferences), 1)

	assert.Equal(t, expectedAf[0].ObjectMeta.Name, actualFs.ObjectMeta.Name)
	assert.Equal(t, expectedAf[0].Spec, actualFs.Spec)
}
