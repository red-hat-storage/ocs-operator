package storagecluster

import (
	"context"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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
			Name:      util.GenerateNameForCephBlockPool(cr.Name),
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
			Name:      util.GenerateNameForCephNFSBlockPool(cr),
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