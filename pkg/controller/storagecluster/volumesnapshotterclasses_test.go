package storagecluster

import (
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVolumeSnapshotterClasses(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)
		assertVolumeSnapshotterClasses(t, reconciler, cr, request)
	}
}

func assertVolumeSnapshotterClasses(t *testing.T, reconciler ReconcileStorageCluster,
	cr *api.StorageCluster, request reconcile.Request) {
	rbdVSCName := "ocsinit-rbdplugin-snapclass"
	cephfsVSCName := "ocsinit-cephfsplugin-snapclass"
	vscNames := []string{cephfsVSCName, rbdVSCName}
	for _, eachVSCName := range vscNames {
		actualVSC := &snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: eachVSCName,
			},
		}
		request.Name = eachVSCName
		err := reconciler.client.Get(nil, request.NamespacedName, actualVSC)
		assert.NoError(t, err)
	}
}
