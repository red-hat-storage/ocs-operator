package storagecluster

import (
	"context"
	"testing"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/openshift/ocs-operator/api/v1"
)

func TestCephRbdMirror(t *testing.T) {
	//cases for testing
	var cases = []struct {
		label                string
		createRuntimeObjects bool
		spec                 *api.StorageClusterSpec
	}{
		{
			label:                "create-ceph-rbd-mirror",
			createRuntimeObjects: false,
			spec: &api.StorageClusterSpec{
				Mirroring: api.MirroringSpec{
					Enabled: true,
				},
			},
		},
		{
			label:                "delete-ceph-rbd-mirror",
			createRuntimeObjects: false,
			spec: &api.StorageClusterSpec{
				Mirroring: api.MirroringSpec{
					Enabled: false,
				},
			},
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		for _, c := range cases {
			var objects []client.Object
			t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
				t, cp, objects, c.spec)
			if c.createRuntimeObjects {
				objects = createUpdateRuntimeObjects(t, cp, reconciler) //nolint:staticcheck //no need to use objects as they update in runtime
			}
			switch c.label {
			case "create-ceph-rbd-mirror":
				assertCephRbdMirrorCreation(t, reconciler, cr, request)
			case "delete-ceph-rbd-mirror":
				assertCephRbdMirrorDeletion(t, reconciler, cr, request)
			}
		}
	}
}

func assertCephRbdMirrorCreation(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualCrm := &cephv1.CephRBDMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephrbdmirror",
		},
	}
	request.Name = "ocsinit-cephrbdmirror"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCrm)
	assert.NoError(t, err)

	expectedCrm, err := reconciler.newCephRbdMirrorInstances(cr)
	assert.NoError(t, err)

	assert.Equal(t, len(expectedCrm[0].OwnerReferences), 1)

	assert.Equal(t, expectedCrm[0].ObjectMeta.Name, actualCrm.ObjectMeta.Name)
	assert.Equal(t, expectedCrm[0].Spec, actualCrm.Spec)
}

func assertCephRbdMirrorDeletion(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	actualCrm := &cephv1.CephRBDMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit-cephrbdmirror",
		},
	}
	request.Name = "ocsinit-cephrbdmirror"
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualCrm)
	assert.Error(t, err)
}
