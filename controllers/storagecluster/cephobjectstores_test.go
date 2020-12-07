package storagecluster

import (
	"context"
	"testing"

	api "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
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
			var objects []runtime.Object
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(cp)
			}
			t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
				t, cp, objects)
			assertCephObjectStores(t, reconciler, cr, request)
		}
	}
}
func assertCephObjectStores(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephObjectStoreInstances(cr)
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
	if avoidObjectStore(reconciler.Platform.platform) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCos[0].ObjectMeta.Name, actualCos.ObjectMeta.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Condition(
			t, func() bool { return expectedCos[0].Spec.Gateway.Instances > 1 },
			"there should be multiple 'Spec.Gateway.Instances'")
		assert.Equal(
			t, expectedCos[0].Spec.Gateway.Placement, getPlacement(cr, "rgw"))
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)
}
