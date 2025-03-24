package storagecluster

import (
	"context"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/stretchr/testify/assert"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVolumeSnapshotterClasses(t *testing.T) {
	t, reconciler, _, request := initStorageClusterResourceCreateUpdateTest(t, nil, nil)
	assertVolumeSnapshotterClasses(t, reconciler, request)
}

func assertVolumeSnapshotterClasses(t *testing.T, reconciler StorageClusterReconciler,
	request reconcile.Request) {
	cephnfsVSCName := "ocsinit-nfsplugin-snapclass"
	vscNames := []string{cephnfsVSCName}
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
