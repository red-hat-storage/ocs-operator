package storagecluster

import (
	"context"
	"fmt"
	"testing"

	api "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type CephObjectStoreTestCase struct {
	label                string
	createRuntimeObjects bool
	gatewayInstances     int32
}

func TestCephObjectStores(t *testing.T) {
	var cases = []CephObjectStoreTestCase{
		{
			label:                "case 1", // Ensure that cephObjectStores are created on non-cloud Platform with default values
			createRuntimeObjects: false,
			gatewayInstances:     1,
		},
		{
			label:                "case 2", // Ensure that cephObjectStores are created on non-cloud Platform with configured values
			createRuntimeObjects: false,
			gatewayInstances:     2,
		},
	}
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			var objects []runtime.Object
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(cp)
			}
			cr := createDefaultStorageCluster()
			if c.gatewayInstances != 1 {
				cr.Spec.ManagedResources.CephObjectStores.GatewayInstances = c.gatewayInstances
			}
			t, reconciler, request := initStorageClusterResourceCreateUpdateTestWithPlatformAndSC(
				t, cp, objects, cr)
			assertCephObjectStores(t, c, reconciler, cr, request)
		}
	}
}

func assertCephObjectStores(t *testing.T, c CephObjectStoreTestCase, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	expectedCos, err := reconciler.newCephObjectStoreInstances(cr, reconciler.Log)
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
	if avoidObjectStore(reconciler.platform.platform) {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		assert.Equal(t, expectedCos[0].ObjectMeta.Name, actualCos.ObjectMeta.Name)
		assert.Equal(t, expectedCos[0].Spec, actualCos.Spec)
		assert.Condition(
			t, func() bool { return expectedCos[0].Spec.Gateway.Instances == c.gatewayInstances },
			c.label, fmt.Sprintf("there should be %d 'Spec.Gateway.Instances'", c.gatewayInstances))
		assert.Equal(
			t, expectedCos[0].Spec.Gateway.Placement, getPlacement(cr, "rgw"))
	}

	assert.Equal(t, len(expectedCos[0].OwnerReferences), 1)
}
