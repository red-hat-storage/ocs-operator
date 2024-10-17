package storagecluster

import (
	"context"
	"testing"

	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestVolumeGroupSnapshotterClasses(t *testing.T) {
	t, reconciler, _, request := initStorageClusterResourceCreateUpdateTest(t, nil, nil)
	assertVolumeGroupSnapshotterClasses(t, reconciler, request)
}

func assertVolumeGroupSnapshotterClasses(t *testing.T, reconciler StorageClusterReconciler,
	request reconcile.Request) {
	rbdVSCName := "ocsinit-rbdplugin-groupsnapclass"
	cephfsVSCName := "ocsinit-cephfsplugin-groupsnapclass"
	vscNames := []string{cephfsVSCName, rbdVSCName}
	for _, eachVSCName := range vscNames {
		actualVSC := &groupsnapapi.VolumeGroupSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: eachVSCName,
			},
		}
		request.Name = eachVSCName
		err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actualVSC)
		assert.NoError(t, err)
	}
}
