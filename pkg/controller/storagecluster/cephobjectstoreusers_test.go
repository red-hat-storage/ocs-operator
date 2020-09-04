package storagecluster

import (
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

func TestCephObjectStoreUsers(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, nil)
		assertCephObjectStoreUsers(t, reconciler, cr, request)
	}

}

func assertCephObjectStoreUsers(t *testing.T, reconciler ReconcileStorageCluster, cr *api.StorageCluster, request reconcile.Request) {
	expectedCosu, err := reconciler.newCephObjectStoreUserInstances(cr)
	assert.NoError(t, err)

	actualCosu := &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephobjectstoreuser",
		},
	}
	request.Name = "ocsinit-cephobjectstoreuser"
	err = reconciler.client.Get(nil, request.NamespacedName, actualCosu)
	if isValidCloudPlatform(reconciler.platform.platform) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCosu[0].ObjectMeta.Name, actualCosu.ObjectMeta.Name)
		assert.Equal(t, expectedCosu[0].Spec, actualCosu.Spec)
	}

	assert.Equal(t, len(expectedCosu[0].OwnerReferences), 1)

}
