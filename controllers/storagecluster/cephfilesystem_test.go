package storagecluster

import (
	"context"
	"testing"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/red-hat-storage/ocs-operator/v4/api/v1"
)

func TestCephFileSystem(t *testing.T) {
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
			assertCephFileSystem(t, reconciler, cr, request)
		}
	}

}

func assertCephFileSystem(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualFs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
	}
	request.Name = "ocsinit-cephfilesystem"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualFs)
	assert.NoError(t, err)

	expectedAf, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedAf[0].OwnerReferences), 1)

	assert.Equal(t, expectedAf[0].ObjectMeta.Name, actualFs.ObjectMeta.Name)
	assert.Equal(t, expectedAf[0].Spec, actualFs.Spec)

	actualFsSVG := &cephv1.CephFilesystemSubVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem-subvolumegroup",
		},
	}
	actualFs.Namespace = request.Namespace
	request.Name = "ocsinit-cephfilesystem-subvolumegroup"
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actualFsSVG)
	assert.NoError(t, err)

	expectedAfSVG := newCephFileSystemSubVolumeGroup(actualFs)
	assert.NoError(t, err)
	assert.Equal(t, 1, len(actualFsSVG.OwnerReferences))
	assert.Equal(t, expectedAfSVG.ObjectMeta.Name, actualFsSVG.ObjectMeta.Name)
	assert.Equal(t, expectedAfSVG.Spec, actualFsSVG.Spec)
}
