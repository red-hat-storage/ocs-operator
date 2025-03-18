package storagecluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

var (
	testPeerSecretName = "peer-cluster-token"
)

func TestCephBlockPools(t *testing.T) {
	//cases for testing
	var cases = []struct {
		label                string
		createRuntimeObjects bool
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
		assertCephBlockPools(t, reconciler, cr, request, false, false)
		assertCephNFSBlockPool(t, reconciler, cr, request)
	}
}

func getInitData(customSpec *api.StorageClusterSpec) *api.StorageCluster {
	cr := createDefaultStorageCluster()
	if customSpec != nil {
		_ = mergo.Merge(&cr.Spec, customSpec)
	}
	return cr
}

func createReconcilerFromCustomResources(t *testing.T, cr *api.StorageCluster) StorageClusterReconciler {
	reconciler := createFakeInitializationStorageClusterReconciler(
		t, &nbv1.NooBaa{})

	secret := corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: testPeerSecretName,
		},
	}

	clientObjs := []client.Object{cr, &secret}

	for _, obj := range clientObjs {
		if err := reconciler.Client.Create(context.TODO(), obj); err != nil {
			t.Fatalf("failed to create a needed runtime object: %v", err)
		}
	}
	return reconciler
}

func assertCephBlockPools(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request, mirroringEnabled bool, validSecret bool) {
	actualCbp := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephblockpool",
		},
	}
	request.Name = "ocsinit-cephblockpool"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCbp)
	assert.NoError(t, err)

	expectedCbp := cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateNameForCephBlockPool(cr),
			Namespace: cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: cr.UID,
				},
			},
		},
		Spec: cephv1.NamedBlockPoolSpec{
			PoolSpec: cephv1.PoolSpec{
				DeviceClass:        cr.Status.DefaultCephDeviceClass,
				EnableCrushUpdates: true,
				FailureDomain:      getFailureDomain(cr),
				Replicated:         generateCephReplicatedSpec(cr, poolTypeData),
				EnableRBDStats:     true,
				Parameters: map[string]string{
					"bulk": "true",
				},
			},
		},
	}

	if mirroringEnabled {
		expectedCbp.Spec.Mirroring.Enabled = true
		expectedCbp.Spec.Mirroring.Mode = "image"
		expectedSecretNames := []string(nil)
		if validSecret {
			expectedSecretNames = []string{testPeerSecretName}
		}
		expectedCbp.Spec.Mirroring.Peers = &cephv1.MirroringPeerSpec{SecretNames: expectedSecretNames}
	}

	assert.Equal(t, len(expectedCbp.OwnerReferences), 1)

	assert.Equal(t, expectedCbp.ObjectMeta.Name, actualCbp.ObjectMeta.Name)
	assert.Equal(t, expectedCbp.Spec, actualCbp.Spec)
}

func assertCephNFSBlockPool(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualNFSBlockPool := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephnfs-builtin-pool",
		},
	}
	request.Name = "ocsinit-cephnfs-builtin-pool"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualNFSBlockPool)
	assert.NoError(t, err)

	expectedCbp := cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephNFSBlockPool(cr),
			Namespace: cr.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID: cr.UID,
				},
			},
		},
		Spec: cephv1.NamedBlockPoolSpec{
			PoolSpec: cephv1.PoolSpec{
				DeviceClass:        cr.Status.DefaultCephDeviceClass,
				EnableCrushUpdates: true,
				FailureDomain:      getFailureDomain(cr),
				Replicated:         generateCephReplicatedSpec(cr, poolTypeMetadata),
				EnableRBDStats:     true,
			},
			Name: ".nfs",
		},
	}

	assert.Equal(t, len(expectedCbp.OwnerReferences), 1)
	assert.Equal(t, expectedCbp.ObjectMeta.Name, actualNFSBlockPool.ObjectMeta.Name)
	assert.Equal(t, expectedCbp.Spec, actualNFSBlockPool.Spec)
}

func TestBulkFlagBehaviorCephBlockPool(t *testing.T) {
	var cases = []struct {
		description        string
		existingPool       *cephv1.CephBlockPool
		storageClusterSpec *api.StorageClusterSpec
		expectedBulk       string
	}{
		{
			description:  "case 1: New pool creation - bulk flag should be set automatically",
			expectedBulk: "true",
		},
		{
			description: "case 2: New pool creation, CR specifies bulk flag false - should respect CR setting",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephBlockPools: api.ManageCephBlockPools{
						PoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "false",
							},
						},
					},
				},
			},
			expectedBulk: "false",
		},
		{
			description: "case 3: Existing pool with bulk flag - should preserve flag",
			existingPool: &cephv1.CephBlockPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "ocsinit-cephblockpool",
					CreationTimestamp: metav1.Now(),
				},
				Spec: cephv1.NamedBlockPoolSpec{
					PoolSpec: cephv1.PoolSpec{
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
				},
			},
			expectedBulk: "true",
		},
		{
			description: "case 4: Existing pool without bulk flag - should not set flag",
			existingPool: &cephv1.CephBlockPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "ocsinit-cephblockpool",
					CreationTimestamp: metav1.Now(),
				},
				Spec: cephv1.NamedBlockPoolSpec{
					PoolSpec: cephv1.PoolSpec{},
				},
			},
			expectedBulk: "",
		},
		{
			description: "case 5: Existing pool without bulk flag - CR specifies bulk flag true - should respect CR setting",
			existingPool: &cephv1.CephBlockPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:              "ocsinit-cephblockpool",
					CreationTimestamp: metav1.Now(),
				},
				Spec: cephv1.NamedBlockPoolSpec{
					PoolSpec: cephv1.PoolSpec{},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephBlockPools: api.ManageCephBlockPools{
						PoolSpec: &cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedBulk: "true",
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

		if c.existingPool != nil {
			err := reconciler.Client.Create(context.TODO(), c.existingPool)
			assert.NoError(t, err)
		}

		obj := &ocsCephBlockPools{}
		_, err = obj.ensureCreated(&reconciler, cr)
		assert.NoError(t, err)

		actualCbp := &cephv1.CephBlockPool{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "ocsinit-cephblockpool"}, actualCbp)
		assert.NoError(t, err)

		bulkValue, exists := actualCbp.Spec.Parameters["bulk"]
		if c.expectedBulk == "" {
			assert.False(t, exists, "bulk parameter should not exist")
		} else {
			assert.True(t, exists, "bulk parameter should exist")
			assert.Equal(t, c.expectedBulk, bulkValue, "bulk parameter value mismatch")
		}
	}
}

func TestBulkFlagBehaviorNonResilientCephBlockPool(t *testing.T) {
	var cases = []struct {
		description        string
		existingPools      []*cephv1.CephBlockPool
		storageClusterSpec *api.StorageClusterSpec
		expectedBulk       string
		expectedPgNum      string
		expectedPgpNum     string
	}{
		{
			description:  "case 1: New non-resilient pool creation - bulk flag should be set automatically",
			expectedBulk: "true",
		},
		{
			description: "case 2: New non-resilient pool creation, CR specifies bulk flag false - should respect CR setting and set pg numbers",
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephNonResilientPools: api.ManageCephNonResilientPools{
						Enable: true,
						Parameters: map[string]string{
							"bulk": "false",
						},
					},
				},
			},
			expectedBulk:   "false",
			expectedPgNum:  "16",
			expectedPgpNum: "16",
		},
		{
			description: "case 3: Existing non-resilient pools with bulk flag - should preserve flag",
			existingPools: []*cephv1.CephBlockPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone1",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone2",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone3",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							Parameters: map[string]string{
								"bulk": "true",
							},
						},
					},
				},
			},
			expectedBulk: "true",
		},
		{
			description: "case 4: Existing non-resilient pools without bulk flag - should not set flag but set pg numbers",
			existingPools: []*cephv1.CephBlockPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone1",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone2",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone3",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
			},
			expectedBulk:   "",
			expectedPgNum:  "16",
			expectedPgpNum: "16",
		},
		{
			description: "case 5: Existing non-resilient pools without bulk flag - CR specifies bulk flag true - should respect CR setting",
			existingPools: []*cephv1.CephBlockPool{
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone1",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone2",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
				{
					ObjectMeta: metav1.ObjectMeta{
						Name:              "ocsinit-nonresilient-zone3",
						CreationTimestamp: metav1.Now(),
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{},
					},
				},
			},
			storageClusterSpec: &api.StorageClusterSpec{
				ManagedResources: api.ManagedResourcesSpec{
					CephNonResilientPools: api.ManageCephNonResilientPools{
						Enable: true,
						Parameters: map[string]string{
							"bulk": "true",
						},
					},
				},
			},
			expectedBulk: "true",
		},
	}

	for _, c := range cases {
		t.Logf("Running %s", c.description)
		var objects []runtime.Object
		reconciler := createFakeStorageClusterReconciler(t, objects...)
		cr := createDefaultStorageCluster()

		cr.Spec.ManagedResources.CephNonResilientPools.Enable = true
		cr.Status.FailureDomainKey, cr.Status.FailureDomainValues = cr.Status.NodeTopologies.GetKeyValues(cr.Status.FailureDomain)
		if c.storageClusterSpec != nil {
			_ = mergo.Merge(&cr.Spec, c.storageClusterSpec)
		}
		err := reconciler.Client.Create(context.TODO(), cr)
		assert.NoError(t, err)

		if c.existingPools != nil {
			for _, pool := range c.existingPools {
				err := reconciler.Client.Create(context.TODO(), pool)
				assert.NoError(t, err)
			}
		}

		obj := &ocsCephBlockPools{}
		_, err = obj.ensureCreated(&reconciler, cr)
		assert.NoError(t, err)

		// Verify each failure domain has a pool with correct settings
		for _, failureDomain := range cr.Status.FailureDomainValues {
			poolName := fmt.Sprintf("ocsinit-nonresilient-%s", failureDomain)
			actualCbp := &cephv1.CephBlockPool{}
			err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: poolName, Namespace: cr.Namespace}, actualCbp)
			assert.NoError(t, err)

			// Verify bulk flag
			bulkValue, exists := actualCbp.Spec.Parameters["bulk"]
			if c.expectedBulk == "" {
				assert.False(t, exists, "bulk parameter should not exist for pool %s", poolName)
			} else {
				assert.True(t, exists, "bulk parameter should exist for pool %s", poolName)
				assert.Equal(t, c.expectedBulk, bulkValue, "bulk parameter value mismatch for pool %s", poolName)
			}

			// Verify pg numbers when bulk is false or not set
			if c.expectedBulk == "false" || c.expectedBulk == "" {
				pgNum, exists := actualCbp.Spec.Parameters["pg_num"]
				assert.True(t, exists, "pg_num parameter should exist for pool %s", poolName)
				assert.Equal(t, c.expectedPgNum, pgNum, "pg_num parameter value mismatch for pool %s", poolName)

				pgpNum, exists := actualCbp.Spec.Parameters["pgp_num"]
				assert.True(t, exists, "pgp_num parameter should exist for pool %s", poolName)
				assert.Equal(t, c.expectedPgpNum, pgpNum, "pgp_num parameter value mismatch for pool %s", poolName)
			}
		}
	}
}
