package storagecluster

import (
	"context"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVolumeSnapshotterClasses(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil, nil)
		assertVolumeSnapshotterClasses(t, reconciler, cr, request)
	}
}

func assertVolumeSnapshotterClasses(t *testing.T, reconciler StorageClusterReconciler,
	cr *api.StorageCluster, request reconcile.Request) {
	rbdVSCName := "ocsinit-rbdplugin-snapclass"
	cephfsVSCName := "ocsinit-cephfsplugin-snapclass"
	cephnfsVSCName := "ocsinit-nfsplugin-snapclass"
	vscNames := []string{cephfsVSCName, rbdVSCName, cephnfsVSCName}
	for _, eachVSCName := range vscNames {
		actualVSC := &snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: eachVSCName,
			},
		}
		request.Name = eachVSCName
		err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualVSC)
		assert.NoError(t, err)
	}
}
