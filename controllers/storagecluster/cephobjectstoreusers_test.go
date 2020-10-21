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

func TestCephObjectStoreUsers(t *testing.T) {
	var cases = []struct {
		label                string
		createRuntimeObjects bool
	}{
		{
			label:                "case 1", // Ensure creation of CephObjectStoreUsers on Non-Cloud Platform
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
			assertCephObjectStoreUsers(t, reconciler, cr, request)
		}
	}

}

func assertCephObjectStoreUsers(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCosu, err := reconciler.newCephObjectStoreUserInstances(cr)
	assert.NoError(t, err)

	actualCosu := &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstoreuser",
		},
	}
	request.Name = "ocsinit-cephobjectstoreuser"
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCosu)
	if avoidObjectStore(reconciler.platform.platform) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCosu[0].ObjectMeta.Name, actualCosu.ObjectMeta.Name)
		assert.Equal(t, expectedCosu[0].Spec, actualCosu.Spec)
	}

	assert.Equal(t, len(expectedCosu[0].OwnerReferences), 1)

}
