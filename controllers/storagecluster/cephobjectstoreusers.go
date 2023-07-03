package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephObjectStoreUsers struct{}

const prometheusUserName = "prometheus-user"

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
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      prometheusUserName,
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreUserSpec{
				DisplayName: prometheusUserName,
				Store:       generateNameForCephObjectStore(initData),
				// This user needs to read quota info from other users where actual quota is on set at the backend
				Capabilities: &cephv1.ObjectUserCapSpec{
					User: "read",
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set Controller Reference for CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(initData.Namespace, generateNameForCephObjectStore(initData)))
			return nil, err
		}
	}
	return ret, nil
}

// ensureCreated ensures that cephObjectStoreUser resources exist in the desired
// state.
func (obj *ocsCephObjectStoreUsers) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStoreUsers.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}
	skip, err := r.PlatformsShouldSkipObjectStore()
	if err != nil {
		return reconcile.Result{}, err
	}

	if skip {
		platform, err := r.platform.GetPlatform(r.Client)
		if err != nil {
			return reconcile.Result{}, err
		}
		r.Log.Info("Platform is set to skip object store. Not creating a CephObjectStoreUser.", "Platform", platform)
		return reconcile.Result{}, nil
	}

	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(instance)
	if err != nil {
		r.Log.Error(err, "Unable to create instances for CephObjectStoreUsers.")
		return reconcile.Result{}, err
	}
	err = r.createCephObjectStoreUsers(cephObjectStoreUsers, instance)
	if err != nil {
		r.Log.Error(err, "Could not create CephObjectStoresUsers.")
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, err
}

// ensureDeleted deletes the CephObjectStoreUsers owned by the StorageCluster
func (obj *ocsCephObjectStoreUsers) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	foundCephObjectStoreUser := &cephv1.CephObjectStoreUser{}
	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(sc)
	if err != nil {
		r.Log.Error(err, "Cannot create CephObjectStoreUser instances.")
		return reconcile.Result{}, err
	}

	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStoreUser not found.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve CephObjectStoreUser %v: %v", cephObjectStoreUser.Name, err)
		}

		if cephObjectStoreUser.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
			err = r.Client.Delete(context.TODO(), foundCephObjectStoreUser)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephObjectStoreUser %v: %v", foundCephObjectStoreUser.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStoreUser is deleted.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
				continue
			}
		}
		r.Log.Error(err, "Uninstall: Waiting for CephObjectStoreUser to be deleted.", "CephObjectStoreUser", klog.KRef(sc.Namespace, cephObjectStoreUser.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephObjectStoreUser %v to be deleted", cephObjectStoreUser.Name)

	}
	return reconcile.Result{}, nil
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
				r.Log.Info("Unable to restore CephObjectStoreUser because it is marked for deletion.", "CephObjectStoreUser", klog.KRef(cephObjectStoreUser.Namespace, existing.Name))
				return fmt.Errorf("failed to restore CephObjectStoreUser %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info("Restoring original CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(cephObjectStoreUser.Namespace, cephObjectStoreUser.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStoreUser.ObjectMeta.OwnerReferences
			cephObjectStoreUser.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephObjectStoreUser)
			if err != nil {
				r.Log.Error(err, "Unable to update CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(cephObjectStoreUser.Namespace, cephObjectStoreUser.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(cephObjectStoreUser.Namespace, cephObjectStoreUser.Name))
			err = r.Client.Create(context.TODO(), cephObjectStoreUser)
			if err != nil {
				r.Log.Error(err, "Could not create CephObjectStoreUser.", "CephObjectStoreUser", klog.KRef(cephObjectStoreUser.Namespace, cephObjectStoreUser.Name))
				return err
			}
		}
	}
	return nil
}
