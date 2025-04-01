package storagecluster

import (
	"context"
	"testing"

	"github.com/imdario/mergo"
	configv1 "github.com/openshift/api/config/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	expectedCos[0].Spec.MetadataPool.Parameters = map[string]string{
		"bulk": "true",
	}
	expectedCos[0].Spec.DataPool.Parameters = map[string]string{
		"bulk": "true",
	}

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

func TestBulkFlagBehaviorCephObjectStore(t *testing.T) {
	var cases = []struct {
		description          string
		existingStore        *cephv1.CephObjectStore
		storageClusterSpec   *api.StorageClusterSpec
		expectedMetadataBulk string
		expectedDataBulk     string
	}{
		{
			description:          "case 1: New object store creation - bulk flag should be set automatically for both pools",
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 2: New object store creation, CR specifies bulk flag false - should respect CR setting",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						MetadataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "false",
							},
						},
						DataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "false",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "false",
			expectedDataBulk:     "false",
		},
		{
			description: "case 3: Existing object store with bulk flags - should preserve flags",
			existingStore: &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephobjectstore",
				},
				Spec: cephv1.ObjectStoreSpec{
					MetadataPool: cephv1.PoolSpec{
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
					DataPool: cephv1.PoolSpec{
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 4: Existing object store without bulk flags - should not set flags",
			existingStore: &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephobjectstore",
				},
				Spec: cephv1.ObjectStoreSpec{
					MetadataPool: cephv1.PoolSpec{},
					DataPool:     cephv1.PoolSpec{},
				},
			},
			expectedMetadataBulk: "",
			expectedDataBulk:     "",
		},
		{
			description: "case 5: Existing object store without bulk flags - CR specifies bulk flags - should respect CR setting",
			existingStore: &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephobjectstore",
				},
				Spec: cephv1.ObjectStoreSpec{
					MetadataPool: cephv1.PoolSpec{},
					DataPool:     cephv1.PoolSpec{},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						MetadataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
						DataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 6: New object store creation - only metadata pool bulk flag should be set",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						MetadataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
						DataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "false",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "false",
		},
		{
			description: "case 7: New object store creation - only data pool bulk flag should be set",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						MetadataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "false",
							},
						},
						DataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "false",
			expectedDataBulk:     "true",
		},
		{
			description: "case 8: Existing object store - preserve metadata pool bulk flag, set data pool bulk flag",
			existingStore: &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephobjectstore",
				},
				Spec: cephv1.ObjectStoreSpec{
					MetadataPool: cephv1.PoolSpec{
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
					DataPool: cephv1.PoolSpec{},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						DataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 9: Existing object store - preserve data pool bulk flag, set metadata pool bulk flag",
			existingStore: &cephv1.CephObjectStore{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephobjectstore",
				},
				Spec: cephv1.ObjectStoreSpec{
					MetadataPool: cephv1.PoolSpec{},
					DataPool: cephv1.PoolSpec{
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephObjectStores: api.ManageCephObjectStores{
						MetadataPoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
	}

	for _, c := range cases {
		t.Logf("Running %s", c.description)
		var objects []runtime.Object
		reconciler := createFakeStorageClusterReconciler(t, objects...)
		cr := createDefaultStorageCluster()
		if c.storageClusterSpec != nil {
			_ = mergo.Merge(&cr.Spec, c.storageClusterSpec)
		}
		err := reconciler.Client.Create(context.TODO(), cr)
		assert.NoError(t, err)

		if c.existingStore != nil {
			err := reconciler.Client.Create(context.TODO(), c.existingStore)
			assert.NoError(t, err)
		}

		obj := &ocsCephObjectStores{}
		_, err = obj.ensureCreated(&reconciler, cr)
		assert.NoError(t, err)

		actualStore := &cephv1.CephObjectStore{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "ocsinit-cephobjectstore"}, actualStore)
		assert.NoError(t, err)

		// Check metadata pool bulk flag
		metadataBulkValue, metadataExists := actualStore.Spec.MetadataPool.Parameters["bulk"]
		if c.expectedMetadataBulk == "" {
			assert.False(t, metadataExists, "metadata pool bulk parameter should not exist")
		} else {
			assert.True(t, metadataExists, "metadata pool bulk parameter should exist")
			assert.Equal(t, c.expectedMetadataBulk, metadataBulkValue, "metadata pool bulk parameter value mismatch")
		}

		// Check data pool bulk flag
		dataBulkValue, dataExists := actualStore.Spec.DataPool.Parameters["bulk"]
		if c.expectedDataBulk == "" {
			assert.False(t, dataExists, "data pool bulk parameter should not exist")
		} else {
			assert.True(t, dataExists, "data pool bulk parameter should exist")
			assert.Equal(t, c.expectedDataBulk, dataBulkValue, "data pool bulk parameter value mismatch")
		}
	}
}

func TestCephObjectReadAffinity(t *testing.T) {
	storageCluster := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sc",
		},

		Spec: api.StorageClusterSpec{
			StorageDeviceSets: []api.StorageDeviceSet{
				{
					Name: "set-1",
				},
			},
		},
	}

	// 1. No readAffinity should be set in case of portable OSDs.
	storageCluster.Spec.StorageDeviceSets[0].Portable = true
	reconciler := createReconcilerFromCustomResources(t, storageCluster)
	objectStores, err := reconciler.newCephObjectStoreInstances(storageCluster, nil)
	assert.NoError(t, err)
	for _, objectStore := range objectStores {
		assert.Nil(t, objectStore.Spec.Gateway.ReadAffinity)
	}

	// 2. Localize readAffinity should be set in case of non-portable OSDs.
	storageCluster.Spec.StorageDeviceSets[0].Portable = false

	objectStores, err = reconciler.newCephObjectStoreInstances(storageCluster, nil)
	assert.NoError(t, err)
	for _, objectStore := range objectStores {
		assert.NotNil(t, objectStore.Spec.Gateway.ReadAffinity)
		assert.Equal(t, "localize", objectStore.Spec.Gateway.ReadAffinity.Type)
	}
}

func TestCephObjectStoreReadAffinityOnExistingClusters(t *testing.T) {
	objStores := &ocsCephObjectStores{}
	storageCluster := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sc-1",
		},

		Spec: api.StorageClusterSpec{
			StorageDeviceSets: []api.StorageDeviceSet{
				{
					Name:     "set-1",
					Portable: false,
				},
			},
		},
	}
	reconciler := createReconcilerFromCustomResources(t, storageCluster)

	// 1. Existing cephObjectStores should use localize readAffinity for non-portable OSDs.
	newObjectStore := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: "sc-1-cephobjectstore",
		},
		Spec: cephv1.ObjectStoreSpec{},
	}

	actualObjectStore := &cephv1.CephObjectStore{}

	err := reconciler.Client.Create(context.TODO(), newObjectStore)
	assert.NoError(t, err)
	_, err = objStores.ensureCreated(&reconciler, storageCluster)
	assert.NoError(t, err)
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "sc-1-cephobjectstore"}, actualObjectStore)
	assert.NoError(t, err)
	assert.NotNil(t, actualObjectStore.Spec.Gateway.ReadAffinity)
	assert.Equal(t, "localize", actualObjectStore.Spec.Gateway.ReadAffinity.Type)

	// 2. Ensure that change in the readAffinity of existing object stores should be preserved across the reconciles
	actualObjectStore.Spec.Gateway.ReadAffinity.Type = "balance"
	err = reconciler.Client.Update(context.TODO(), actualObjectStore)
	assert.NoError(t, err)
	_, err = objStores.ensureCreated(&reconciler, storageCluster)
	assert.NoError(t, err)
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "sc-1-cephobjectstore"}, actualObjectStore)
	assert.NoError(t, err)
	assert.NotNil(t, actualObjectStore.Spec.Gateway.ReadAffinity)
	assert.Equal(t, "balance", actualObjectStore.Spec.Gateway.ReadAffinity.Type)

	// 3. Ensure that change in the readAffinity of existing object stores should be preserved across the reconciles
	actualObjectStore.Spec.Gateway.ReadAffinity.Type = "default"
	err = reconciler.Client.Update(context.TODO(), actualObjectStore)
	assert.NoError(t, err)
	_, err = objStores.ensureCreated(&reconciler, storageCluster)
	assert.NoError(t, err)
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "sc-1-cephobjectstore"}, actualObjectStore)
	assert.NoError(t, err)
	assert.NotNil(t, actualObjectStore.Spec.Gateway.ReadAffinity)
	assert.Equal(t, "default", actualObjectStore.Spec.Gateway.ReadAffinity.Type)

}
