package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsCephObjectStoreUsers struct{}

// newCephObjectStoreUserInstances returns the cephObjectStoreUser instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephObjectStoreUserInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephObjectStoreUser, error) {
	ret := []*cephv1.CephObjectStoreUser{
		{
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
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCreated ensures that cephObjectStoreUser resources exist in the desired
// state.
func (obj *ocsCephObjectStoreUsers) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStoreUsers.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	avoid, err := r.PlatformsShouldAvoidObjectStore()
	if err != nil {
		return err
	}

	if avoid {
		platform, err := r.platform.GetPlatform(r.Client)
		if err != nil {
			return err
		}
		if reconcileStrategy == ReconcileStrategyForce {
			r.Log.Info("force creating a CephObjectStore")
		} else {
			r.Log.Info(fmt.Sprintf("not creating a CephObjectStoreUsers because the platform is '%s'", platform))
			return nil
		}
	}

	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(instance)
	if err != nil {
		return err
	}
	err = r.createCephObjectStoreUsers(cephObjectStoreUsers, instance)
	if err != nil {
		r.Log.Error(err, "could not create CephObjectStoresUsers")
		return err
	}

	return err
}

// ensureDeleted deletes the CephObjectStoreUsers owned by the StorageCluster
func (obj *ocsCephObjectStoreUsers) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	foundCephObjectStoreUser := &cephv1.CephObjectStoreUser{}
	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(sc)
	if err != nil {
		return err
	}

	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStoreUser not found", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrieve cephObjectStoreUser %v: %v", cephObjectStoreUser.Name, err)
		}

		if cephObjectStoreUser.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting cephObjectStoreUser", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
			err = r.Client.Delete(context.TODO(), foundCephObjectStoreUser)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephObjectStoreUser %v: %v", foundCephObjectStoreUser.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStoreUser is deleted", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephObjectStoreUser %v to be deleted", cephObjectStoreUser.Name)

	}
	return nil
}

// createCephObjectStoreUsers creates CephObjectStoreUsers in the desired state
func (r *StorageClusterReconciler) createCephObjectStoreUsers(cephObjectStoreUsers []*cephv1.CephObjectStoreUser, instance *ocsv1.StorageCluster) error {
	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		existing := cephv1.CephObjectStoreUser{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: cephObjectStoreUser.Namespace}, &existing)
		switch {
		case err == nil:
			reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStoreUsers.ReconcileStrategy)
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				r.Log.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info(fmt.Sprintf("Restoring original cephObjectStoreUser %s", cephObjectStoreUser.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStoreUser.ObjectMeta.OwnerReferences
			cephObjectStoreUser.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info(fmt.Sprintf("Creating cephObjectStoreUser %s", cephObjectStoreUser.Name))
			err = r.Client.Create(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
