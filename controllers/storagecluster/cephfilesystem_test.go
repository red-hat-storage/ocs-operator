package storagecluster

import (
	"context"
	"testing"

	"github.com/imdario/mergo"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
			label:                "case 1",
			createRuntimeObjects: false,
		},
	}

	for _, c := range cases {
		var objects []client.Object
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, objects, nil)

		if c.createRuntimeObjects {
			objects = createUpdateRuntimeObjects(t) //nolint:staticcheck //no need to use objects as they update in runtime
		}
		assertCephFileSystem(t, reconciler, cr, request)
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

func TestGetActiveMetadataServers(t *testing.T) {
	var cases = []struct {
		label                         string
		sc                            *api.StorageCluster
		expectedActiveMetadataServers int
	}{
		{
			label:                         "Default case",
			sc:                            &api.StorageCluster{},
			expectedActiveMetadataServers: defaults.CephFSActiveMetadataServers,
		},
		{
			label: "ActiveMetadataServers is set on the StorageCluster CR Spec",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephFilesystems: api.ManageCephFilesystems{
							ActiveMetadataServers: 2,
						},
					},
				},
			},
			expectedActiveMetadataServers: 2,
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		actualActiveMetadataServers := getActiveMetadataServers(c.sc)
		assert.Equal(t, c.expectedActiveMetadataServers, actualActiveMetadataServers)
	}

}

func TestCephFileSystemDataPools(t *testing.T) {
	mocksc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(mocksc)
	mocksc.Status.FailureDomain = "zone"
	defaultPoolSpec := cephv1.PoolSpec{
		EnableCrushUpdates: true,
		DeviceClass:        mocksc.Status.DefaultCephDeviceClass,
		FailureDomain:      getFailureDomain(mocksc),
		Replicated:         generateCephReplicatedSpec(mocksc, poolTypeData),
	}

	var cases = []struct {
		label             string
		sc                *api.StorageCluster
		expectedDataPools []cephv1.NamedPoolSpec
	}{
		{
			label: "Neither DataPoolSpec nor AdditionalDataPools is set",
			sc:    &api.StorageCluster{},
			expectedDataPools: []cephv1.NamedPoolSpec{
				{
					PoolSpec: defaultPoolSpec,
				},
			},
		},
		{
			label: "DataPoolSpec is set & AdditionalDataPools is not set",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephFilesystems: api.ManageCephFilesystems{
							DataPoolSpec: &cephv1.PoolSpec{
								DeviceClass: "gold",
								Replicated: cephv1.ReplicatedSpec{
									Size:            2,
									TargetSizeRatio: 0.8,
								},
							},
						},
					},
				},
			},
			expectedDataPools: []cephv1.NamedPoolSpec{
				{
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "gold",
						EnableCrushUpdates: true,
						Replicated: cephv1.ReplicatedSpec{
							Size:                     2,
							TargetSizeRatio:          0.8,
							ReplicasPerFailureDomain: defaultPoolSpec.Replicated.ReplicasPerFailureDomain,
						},
						FailureDomain: defaultPoolSpec.FailureDomain,
					},
				},
			},
		},
		{
			label: "DataPoolSpec is not set & One item is set on AdditionalDataPools",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephFilesystems: api.ManageCephFilesystems{
							AdditionalDataPools: []cephv1.NamedPoolSpec{
								{
									Name: "test-1",
									PoolSpec: cephv1.PoolSpec{
										Replicated: cephv1.ReplicatedSpec{
											Size:            2,
											TargetSizeRatio: 0.3,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedDataPools: []cephv1.NamedPoolSpec{
				{
					PoolSpec: defaultPoolSpec,
				},
				{
					Name: "test-1",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        defaultPoolSpec.DeviceClass,
						EnableCrushUpdates: true,
						Replicated: cephv1.ReplicatedSpec{
							Size:                     2,
							TargetSizeRatio:          0.3,
							ReplicasPerFailureDomain: defaultPoolSpec.Replicated.ReplicasPerFailureDomain,
						},
						FailureDomain: defaultPoolSpec.FailureDomain,
					},
				},
			},
		},
		{
			label: "DataPoolSpec is not set & multiple AdditionalDataPools are set",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephFilesystems: api.ManageCephFilesystems{
							AdditionalDataPools: []cephv1.NamedPoolSpec{
								{
									Name: "test-1",
									PoolSpec: cephv1.PoolSpec{
										DeviceClass: "gold",
									},
								},
								{
									Name: "test-2",
									PoolSpec: cephv1.PoolSpec{
										DeviceClass: "silver",
									},
								},
							},
						},
					},
				},
			},
			expectedDataPools: []cephv1.NamedPoolSpec{
				{
					PoolSpec: defaultPoolSpec,
				},
				{
					Name: "test-1",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "gold",
						EnableCrushUpdates: true,
						Replicated:         defaultPoolSpec.Replicated,
						FailureDomain:      defaultPoolSpec.FailureDomain,
					},
				},
				{
					Name: "test-2",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "silver",
						EnableCrushUpdates: true,
						Replicated:         defaultPoolSpec.Replicated,
						FailureDomain:      defaultPoolSpec.FailureDomain,
					},
				},
			},
		},
		{
			label: "DataPoolSpec is set & multiple AdditionalDataPools are set",
			sc: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					ManagedResources: api.ManagedResourcesSpec{
						CephFilesystems: api.ManageCephFilesystems{
							DataPoolSpec: &cephv1.PoolSpec{
								DeviceClass: "gold",
								Replicated: cephv1.ReplicatedSpec{
									TargetSizeRatio: 0.1,
								},
							},
							AdditionalDataPools: []cephv1.NamedPoolSpec{
								{
									Name: "test-1",
									PoolSpec: cephv1.PoolSpec{
										DeviceClass: "silver",
										Replicated: cephv1.ReplicatedSpec{
											Size:            2,
											TargetSizeRatio: 0.25,
										},
									},
								},
								{
									Name: "test-2",
									PoolSpec: cephv1.PoolSpec{
										DeviceClass: "bronze",
										Replicated: cephv1.ReplicatedSpec{
											Size:            2,
											TargetSizeRatio: 0.25,
										},
									},
								},
							},
						},
					},
				},
			},
			expectedDataPools: []cephv1.NamedPoolSpec{
				{
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "gold",
						EnableCrushUpdates: true,
						Replicated: cephv1.ReplicatedSpec{
							Size:                     defaultPoolSpec.Replicated.Size,
							TargetSizeRatio:          0.1,
							ReplicasPerFailureDomain: defaultPoolSpec.Replicated.ReplicasPerFailureDomain,
						},
						FailureDomain: defaultPoolSpec.FailureDomain,
					},
				},
				{
					Name: "test-1",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "silver",
						EnableCrushUpdates: true,
						Replicated: cephv1.ReplicatedSpec{
							Size:                     2,
							TargetSizeRatio:          0.25,
							ReplicasPerFailureDomain: defaultPoolSpec.Replicated.ReplicasPerFailureDomain,
						},
						FailureDomain: defaultPoolSpec.FailureDomain,
					},
				},
				{
					Name: "test-2",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:        "bronze",
						EnableCrushUpdates: true,
						Replicated: cephv1.ReplicatedSpec{
							Size:                     2,
							TargetSizeRatio:          0.25,
							ReplicasPerFailureDomain: defaultPoolSpec.Replicated.ReplicasPerFailureDomain,
						},
						FailureDomain: defaultPoolSpec.FailureDomain,
					},
				},
			},
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		var objects []client.Object
		t, reconciler, _, _ := initStorageClusterResourceCreateUpdateTest(t, objects, nil)
		c.sc.Status.FailureDomain = "zone"
		filesystem, err := reconciler.newCephFilesystemInstances(c.sc)
		assert.NoError(t, err)
		actualDataPools := filesystem[0].Spec.DataPools
		assert.Equal(t, c.expectedDataPools, actualDataPools)
	}
}

func TestBulkFlagBehaviorCephFilesystem(t *testing.T) {
	var cases = []struct {
		description          string
		existingFS           *cephv1.CephFilesystem
		storageClusterSpec   *api.StorageClusterSpec
		expectedMetadataBulk string
		expectedDataBulk     string
	}{
		{
			description:          "case 1: New filesystem creation - bulk flag should be set automatically for both pools",
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 2: New filesystem creation, CR specifies bulk flag false - should respect CR setting",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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
			description: "case 3: Existing filesystem with bulk flags - should preserve flags",
			existingFS: &cephv1.CephFilesystem{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephfilesystem",
				},
				Spec: cephv1.FilesystemSpec{
					MetadataPool: cephv1.NamedPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
					DataPools: []cephv1.NamedPoolSpec{
						{
							PoolSpec: cephv1.PoolSpec{
								Parameters: map[string]string{
									"bulk": "true",
								},
							},
						},
					},
				},
			},
			expectedMetadataBulk: "true",
			expectedDataBulk:     "true",
		},
		{
			description: "case 4: Existing filesystem without bulk flags - should not set flags",
			existingFS: &cephv1.CephFilesystem{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephfilesystem",
				},
				Spec: cephv1.FilesystemSpec{
					MetadataPool: cephv1.NamedPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
					DataPools: []cephv1.NamedPoolSpec{
						{
							PoolSpec: cephv1.PoolSpec{},
						},
					},
				},
			},
			expectedMetadataBulk: "",
			expectedDataBulk:     "",
		},
		{
			description: "case 5: Existing filesystem without bulk flags - CR specifies bulk flags - should respect CR setting",
			existingFS: &cephv1.CephFilesystem{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephfilesystem",
				},
				Spec: cephv1.FilesystemSpec{
					MetadataPool: cephv1.NamedPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
					DataPools: []cephv1.NamedPoolSpec{
						{
							PoolSpec: cephv1.PoolSpec{},
						},
					},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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
			description: "case 6: New filesystem creation - only metadata pool bulk flag should be set",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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
			description: "case 7: New filesystem creation - only data pool bulk flag should be set",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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
			description: "case 8: Existing filesystem - preserve metadata pool bulk flag, set data pool bulk flag",
			existingFS: &cephv1.CephFilesystem{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephfilesystem",
				},
				Spec: cephv1.FilesystemSpec{
					MetadataPool: cephv1.NamedPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
					DataPools: []cephv1.NamedPoolSpec{
						{
							PoolSpec: cephv1.PoolSpec{},
						},
					},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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
			description: "case 9: Existing filesystem - preserve data pool bulk flag, set metadata pool bulk flag",
			existingFS: &cephv1.CephFilesystem{
				ObjectMeta: metav1.ObjectMeta{
					Name: "ocsinit-cephfilesystem",
				},
				Spec: cephv1.FilesystemSpec{
					MetadataPool: cephv1.NamedPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
					DataPools: []cephv1.NamedPoolSpec{
						{
							PoolSpec: cephv1.PoolSpec{
								Parameters: map[string]string{
									"bulk": "true",
								},
							},
						},
					},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephFilesystems: api.ManageCephFilesystems{
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

		if c.existingFS != nil {
			err := reconciler.Client.Create(context.TODO(), c.existingFS)
			assert.NoError(t, err)
		}

		obj := &ocsCephFilesystems{}
		_, err = obj.ensureCreated(&reconciler, cr)
		assert.NoError(t, err)

		actualFS := &cephv1.CephFilesystem{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "ocsinit-cephfilesystem"}, actualFS)
		assert.NoError(t, err)

		// Check metadata pool bulk flag
		metadataBulkValue, metadataExists := actualFS.Spec.MetadataPool.PoolSpec.Parameters["bulk"]
		if c.expectedMetadataBulk == "" {
			assert.False(t, metadataExists, "metadata pool bulk parameter should not exist")
		} else {
			assert.True(t, metadataExists, "metadata pool bulk parameter should exist")
			assert.Equal(t, c.expectedMetadataBulk, metadataBulkValue, "metadata pool bulk parameter value mismatch")
		}

		// Check data pool bulk flag
		dataBulkValue, dataExists := actualFS.Spec.DataPools[0].PoolSpec.Parameters["bulk"]
		if c.expectedDataBulk == "" {
			assert.False(t, dataExists, "data pool bulk parameter should not exist")
		} else {
			assert.True(t, dataExists, "data pool bulk parameter should exist")
			assert.Equal(t, c.expectedDataBulk, dataBulkValue, "data pool bulk parameter value mismatch")
		}
	}
}
