package storagecluster

import (
	"context"
	"testing"

	"github.com/imdario/mergo"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			var objects []client.Object
			t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
				t, cp, objects, nil)
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(t, reconciler) //nolint:staticcheck //no need to use objects as they update in runtime
			}
			assertCephBlockPools(t, reconciler, cr, request, false, false)
			assertCephNFSBlockPool(t, reconciler, cr, request)
		}
	}
}

func TestInjectingPeerTokenToCephBlockPool(t *testing.T) {
	//cases for testing
	var cases = []struct {
		label                string
		createRuntimeObjects bool
		spec                 *api.StorageClusterSpec
	}{
		{
			label:                "test-injecting-peer-token-to-cephblockpool",
			createRuntimeObjects: false,
			spec: &api.StorageClusterSpec{
				Mirroring: api.MirroringSpec{
					Enabled:         true,
					PeerSecretNames: []string{testPeerSecretName},
				},
			},
		},
		{
			label:                "test-injecting-empty-peer-token-to-cephblockpool",
			createRuntimeObjects: false,
			spec: &api.StorageClusterSpec{
				Mirroring: api.MirroringSpec{
					Enabled:         true,
					PeerSecretNames: []string{},
				},
			},
		},
		{
			label:                "test-injecting-invalid-peer-token-cephblockpool",
			createRuntimeObjects: false,
			spec: &api.StorageClusterSpec{
				Mirroring: api.MirroringSpec{
					Enabled:         true,
					PeerSecretNames: []string{"wrong-secret-name"},
				},
			},
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			cr := getInitData(c.spec)
			request := reconcile.Request{
				NamespacedName: types.NamespacedName{
					Name:      "ocsinit",
					Namespace: "",
				},
			}
			reconciler := createReconcilerFromCustomResources(t, cp, cr)
			_, err := reconciler.Reconcile(context.TODO(), request)
			assert.NoError(t, err)
			if c.label == "test-injecting-peer-token-to-cephblockpool" {
				assertCephBlockPools(t, reconciler, cr, request, true, true)
			} else {
				assertCephBlockPools(t, reconciler, cr, request, true, false)
			}
		}
	}
}

func getInitData(customSpec *api.StorageClusterSpec) *api.StorageCluster {
	cr := createDefaultStorageCluster()
	if customSpec != nil {
		_ = mergo.Merge(&cr.Spec, customSpec)
	}
	return cr
}

func createReconcilerFromCustomResources(t *testing.T, platform *Platform, cr *api.StorageCluster) StorageClusterReconciler {
	reconciler := createFakeInitializationStorageClusterReconcilerWithPlatform(
		t, platform, &nbv1.NooBaa{})

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
	if mirroringEnabled {
		assert.Equal(t, true, actualCbp.Spec.Mirroring.Enabled)
		assert.Equal(t, "image", actualCbp.Spec.Mirroring.Mode)
		expectedSecretNames := []string(nil)
		if validSecret {
			expectedSecretNames = []string{testPeerSecretName}
		}
		assert.Equal(t, expectedSecretNames, actualCbp.Spec.Mirroring.Peers.SecretNames)
	}

	expectedCbp, err := reconciler.newCephBlockPoolInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedCbp[0].OwnerReferences), 1)

	assert.Equal(t, expectedCbp[0].ObjectMeta.Name, actualCbp.ObjectMeta.Name)
	assert.Equal(t, expectedCbp[0].Spec, actualCbp.Spec)
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

	expectedAf, err := reconciler.newCephBlockPoolInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedAf[1].OwnerReferences), 1)

	assert.Equal(t, expectedAf[1].ObjectMeta.Name, actualNFSBlockPool.ObjectMeta.Name)
	assert.Equal(t, expectedAf[1].Spec, actualNFSBlockPool.Spec)
}
