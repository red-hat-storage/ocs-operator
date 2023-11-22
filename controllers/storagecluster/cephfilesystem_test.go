package storagecluster

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
}

func TestCreateDefaultSubvolumeGroup(t *testing.T) {
	var objects []client.Object
	cp := &Platform{platform: configv1.IBMCloudPlatformType}
	t, reconciler, cr, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, objects, nil)
	filesystem, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	err = reconciler.createDefaultSubvolumeGroup(filesystem[0].Name, filesystem[0].Namespace, filesystem[0].OwnerReferences)
	assert.NoError(t, err)

	svg := &cephv1.CephFilesystemSubVolumeGroup{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: defaultSubvolumeGroupName, Namespace: filesystem[0].Namespace}, svg)
	assert.NoError(t, err) // no error
}

func TestDeleteDefaultSubvolumeGroup(t *testing.T) {
	var objects []client.Object
	cp := &Platform{platform: configv1.IBMCloudPlatformType}
	t, reconciler, cr, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, objects, nil)
	filesystem, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	err = reconciler.deleteDefaultSubvolumeGroup(filesystem[0].Name, filesystem[0].Namespace, filesystem[0].OwnerReferences)
	assert.NoError(t, err)

	svg := &cephv1.CephFilesystemSubVolumeGroup{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: defaultSubvolumeGroupName, Namespace: filesystem[0].Namespace}, svg)
	assert.Error(t, err) // error as csi svg is deleted
}
