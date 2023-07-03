package storagecluster

import (
	"context"
	"testing"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/red-hat-storage/ocs-operator/v4/api/v1"
)

func TestCephNFS(t *testing.T) {
	var cases = []struct {
		label                string
		createRuntimeObjects bool
	}{
		{
			label:                "case 1",
			createRuntimeObjects: false,
		},
	}
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			var objects []client.Object
			t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
				t, cp, objects, nil)
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(t, cp, reconciler) //nolint:staticcheck //no need to use objects as they update in runtime
			}
			assertCephNFS(t, reconciler, cr, request)
			assertCephNFSService(t, reconciler, cr, request)
		}
	}

}

func assertCephNFS(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualNfs := &cephv1.CephNFS{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs",
		},
	}
	request.Name = "ocsinit-cephnfs"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualNfs)
	assert.NoError(t, err)

	expectedAf, err := reconciler.newCephNFSInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedAf[0].OwnerReferences), 1)

	assert.Equal(t, expectedAf[0].ObjectMeta.Name, actualNfs.ObjectMeta.Name)
	assert.Equal(t, expectedAf[0].Spec, actualNfs.Spec)
}

func assertCephNFSService(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualNFSService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs-service",
		},
	}
	request.Name = "ocsinit-cephnfs-service"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualNFSService)
	assert.NoError(t, err)

	expectedAf, err := reconciler.newNFSServices(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedAf[0].OwnerReferences), 1)

	assert.Equal(t, expectedAf[0].ObjectMeta.Name, actualNFSService.ObjectMeta.Name)
	assert.Equal(t, expectedAf[0].Spec, actualNFSService.Spec)
}
