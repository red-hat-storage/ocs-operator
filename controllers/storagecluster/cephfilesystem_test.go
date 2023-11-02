package storagecluster

import (
	"context"
	"strings"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCephFileSystem(t *testing.T) {
	var cases = []struct {
		label                  string
		createRuntimeObjects   bool
		remoteStorageConsumers bool
	}{
		{
			label:                  "Not in provider mode",
			createRuntimeObjects:   false,
			remoteStorageConsumers: false,
		},
		{
			label:                  "In provider mode",
			createRuntimeObjects:   false,
			remoteStorageConsumers: true,
		},
	}
	spList := getMockStorageProfiles()

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			var objects []client.Object

			providerModeSpec := &api.StorageClusterSpec{
				AllowRemoteStorageConsumers:  c.remoteStorageConsumers,
				ProviderAPIServerServiceType: "",
			}

			t, reconcilerOCSInit, cr, requestOCSInit, requestsStorageProfiles := initStorageClusterResourceCreateUpdateTestWithPlatformProviderMode(
				t, cp, objects, providerModeSpec, spList, c.remoteStorageConsumers)
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(t, cp, reconcilerOCSInit) //nolint:staticcheck //no need to use objects as they update in runtime
			}
			if strings.Contains(c.label, "Not") {
				assertCephFileSystem(t, reconcilerOCSInit, cr, requestOCSInit)
			} else {
				assertCephFileSystemProviderMode(t, reconcilerOCSInit, cr, requestOCSInit, requestsStorageProfiles)
			}
		}
	}

}

func assertCephFileSystemProviderMode(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, requestOCSInit reconcile.Request, requestsStorageProfiles []reconcile.Request) {
	actualFs := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephfilesystem",
		},
		Spec: cephv1.FilesystemSpec{
			DataPools: []cephv1.NamedPoolSpec{
				{Name: "fast", PoolSpec: cephv1.PoolSpec{DeviceClass: "fast"}},
				{Name: "med", PoolSpec: cephv1.PoolSpec{DeviceClass: "med"}},
				{Name: "slow", PoolSpec: cephv1.PoolSpec{DeviceClass: "slow"}},
			},
		},
	}
	requestOCSInit.Name = "ocsinit-cephfilesystem"
	err := reconciler.Client.Get(context.TODO(), requestOCSInit.NamespacedName, actualFs)
	assert.NoError(t, err)

	storageProfiles := &api.StorageProfileList{}
	err = reconciler.Client.List(context.TODO(), storageProfiles)
	assert.NoError(t, err)
	assert.Equal(t, len(storageProfiles.Items), len(requestsStorageProfiles))
	assert.Equal(t, len(storageProfiles.Items)-1, len(actualFs.Spec.DataPools))

	expectedCephFS, err := reconciler.newCephFilesystemInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedCephFS[0].OwnerReferences), 1)

	assert.Equal(t, expectedCephFS[0].ObjectMeta.Name, actualFs.ObjectMeta.Name)
	assert.Equal(t, expectedCephFS[0].Spec, actualFs.Spec)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[0].Name, actualFs.Spec.DataPools[0].Name)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[1].Name, actualFs.Spec.DataPools[1].Name)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[2].Name, actualFs.Spec.DataPools[2].Name)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[0].PoolSpec.DeviceClass, actualFs.Spec.DataPools[0].PoolSpec.DeviceClass)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[1].PoolSpec.DeviceClass, actualFs.Spec.DataPools[1].PoolSpec.DeviceClass)
	assert.Equal(t, expectedCephFS[0].Spec.DataPools[2].PoolSpec.DeviceClass, actualFs.Spec.DataPools[2].PoolSpec.DeviceClass)

	for i := range requestsStorageProfiles {
		actualStorageProfile := &api.StorageProfile{}
		requestStorageProfile := requestsStorageProfiles[i]
		err = reconciler.Client.Get(context.TODO(), requestStorageProfile.NamespacedName, actualStorageProfile)
		assert.NoError(t, err)
		assert.Equal(t, requestStorageProfile.Name, actualStorageProfile.Name)

		phaseStorageProfile := api.StorageProfilePhase("")
		if strings.Contains(requestStorageProfile.Name, "blank") {
			phaseStorageProfile = api.StorageProfilePhaseRejected
		}
		assert.Equal(t, phaseStorageProfile, actualStorageProfile.Status.Phase)
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
