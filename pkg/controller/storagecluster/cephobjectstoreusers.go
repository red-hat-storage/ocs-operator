package storagecluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// newCephObjectStoreUserInstances returns the cephObjectStoreUser instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newCephObjectStoreUserInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephObjectStoreUser, error) {
	ret := []*cephv1.CephObjectStoreUser{
		&cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStoreUser(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreUserSpec{
				DisplayName: initData.Name,
				Store:       generateNameForCephObjectStore(initData),
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCephObjectStoreUsers ensures that cephObjectStoreUser resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureCephObjectStoreUsers(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	platform, err := r.platform.GetPlatform(r.client)
	if err != nil {
		return err
	}
	if isValidCloudPlatform(platform) {
		return nil
	}

	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(instance)
	if err != nil {
		return err
	}
	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		existing := cephv1.CephObjectStoreUser{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: cephObjectStoreUser.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStoreUser %s", cephObjectStoreUser.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStoreUser.ObjectMeta.OwnerReferences
			cephObjectStoreUser.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephObjectStoreUser %s", cephObjectStoreUser.Name))
			err = r.client.Create(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		}
	}
	return err
}
