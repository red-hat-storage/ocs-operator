package storagecluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

// deleteStorageClasses deletes the storageClasses that the ocs-operator created
func (r *ReconcileStorageCluster) deleteStorageClasses(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	scs, err := r.newStorageClasses(instance)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Unable to determine the StorageClass names"))
		return nil
	}
	for _, sc := range scs {
		existing := storagev1.StorageClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("StorageClass %s is already marked for deletion", existing.Name))
				break
			}

			reqLogger.Info(fmt.Sprintf("Deleting StorageClass %s", sc.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.client.Delete(context.TODO(), sc)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Ignoring error deleting the StorageClass %s", existing.Name))
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("StorageClass %s not found, nothing to do", sc.Name))
		}
	}
	return nil
}
