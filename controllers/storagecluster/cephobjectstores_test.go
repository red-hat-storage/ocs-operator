package storagecluster

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
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
		platform             configv1.PlatformType
	}{
		{
			label:                "Create CephObjectStore on non Cloud platform",
			createRuntimeObjects: false,
			platform:             configv1.BareMetalPlatformType,
		},
		{
			label:                "Do not create CephObjectStore on Cloud platform",
			createRuntimeObjects: false,
			platform:             configv1.AWSPlatformType,
		},
	}

	for _, c := range cases {
		platform.SetFakePlatformInstanceForTesting(true, c.platform)
		var objects []client.Object
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nil)
		if c.createRuntimeObjects {
			objects = createUpdateRuntimeObjects(t) //nolint:staticcheck //no need to use objects as they update in runtime
		}
		assertCephObjectStores(t, reconciler, cr, request)
		platform.UnsetFakePlatformInstanceForTesting()
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
	skip, skipErr := platform.PlatformsShouldSkipObjectStore()
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
