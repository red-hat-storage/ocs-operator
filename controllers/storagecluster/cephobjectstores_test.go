package storagecluster

import (
	"context"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestCephObjectStores(t *testing.T) {
	var cases = []struct {
		label                string
		createRuntimeObjects bool
	}{
		{
			label:                "case 1", // Ensure that cephObjectStores are created on non-cloud Platform
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
				objects = createUpdateRuntimeObjects(t, reconciler) //nolint:staticcheck //no need to use objects as they update in runtime
			}
			assertCephObjectStores(t, reconciler, cr, request)
		}
	}
}
func assertCephObjectStores(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephObjectStoreInstances(cr, nil)
	assert.NoError(t, err)

	actualCos := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstore",
		},
	}
	request.Name = "ocsinit-cephobjectstore"
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCos)
	// for any cloud platform, 'cephobjectstore' should not be created
	// 'Get' should have thrown an error
	skip, skipErr := reconciler.PlatformsShouldSkipObjectStore()
	assert.NoError(t, skipErr)
	if skip {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCos[0].ObjectMeta.Name, actualCos.ObjectMeta.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Condition(
			t, func() bool { return expectedCos[0].Spec.Gateway.Instances == 1 },
			"there should be one 'Spec.Gateway.Instances' by default")
		assert.Equal(
			t, expectedCos[0].Spec.Gateway.Placement, getPlacement(cr, "rgw"))
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)

	cr.Spec.ManagedResources.CephObjectStores.GatewayInstances = 2
	expectedCos, _ = reconciler.newCephObjectStoreInstances(cr, nil)
	assert.Equal(t, expectedCos[0].Spec.Gateway.Instances, int32(2))
}

func TestGetCephObjectStoreGatewayInstances(t *testing.T) {
	var cases = []struct {
		label                                   string
		sc                                      *api.StorageCluster
		expectedCephObjectStoreGatewayInstances int
	}{
		{
			label:                                   "Default case",
			sc:                                      &api.StorageCluster{},
			expectedCephObjectStoreGatewayInstances: defaults.CephObjectStoreGatewayInstances,
		},
		{
			label: "CephObjectStoreGatewayInstances is set on the StorageCluster CR Spec",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephObjectStores: api.ManageCephObjectStores{
							GatewayInstances: 2,
						},
					},
				},
			},
			expectedCephObjectStoreGatewayInstances: 2,
		},
		{
			label: "Arbiter Mode",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Arbiter: api.ArbiterSpec{
						Enable: true,
					},
				},
			},
			expectedCephObjectStoreGatewayInstances: defaults.ArbiterCephObjectStoreGatewayInstances,
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		actualCephObjectStoreGatewayInstances := getCephObjectStoreGatewayInstances(c.sc)
		assert.Equal(t, c.expectedCephObjectStoreGatewayInstances, actualCephObjectStoreGatewayInstances)
	}
}
