package storagecluster

import (
	"fmt"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVolumeSnapshotterClasses(t *testing.T) {
	var updateVSCObjs = []runtime.Object{
		&snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%splugin-snapclass", "ocsinit", "rbd"),
				Namespace: "",
			},
		},
		&snapapi.VolumeSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name:      fmt.Sprintf("%s-%splugin-snapclass", "ocsinit", "cephfs"),
				Namespace: "",
			},
		},
	}
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)
		assertVolumeSnapshotterClasses(t, reconciler, cr, request)
		// create runtime objects for updation
		updateObjs := createUpdateRuntimeObjects(cp)
		// add the volumesnapshotter classes as well
		updateObjs = append(updateObjs, updateVSCObjs...)
		t, reconciler, cr, request = initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, updateObjs)
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
