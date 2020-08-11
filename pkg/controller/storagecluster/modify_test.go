package storagecluster

import (
	"testing"

	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

func TestUpdate(t *testing.T) {
	var r *ReconcileStorageCluster
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster)
	cr := &v1.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, cr)
	assert.NoError(t, err)
	cr.ObjectMeta.Name = "update-test"
	err = r.StatusUpdate(cr)
	// TODO: How to verify if update worked or not after this line

}
