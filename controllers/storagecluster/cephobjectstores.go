package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsCephObjectStores struct{}

// ensureCreated ensures that CephObjectStore resources exist in the desired
// state.
func (obj *ocsCephObjectStores) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	skip, err := r.PlatformsShouldSkipObjectStore()
	if err != nil {
		return err
	}

	if skip {
		platform, err := r.platform.GetPlatform(r.Client)
		if err != nil {
			return err
		}
		r.Log.Info("Platform is set to skip object store. Not creating a CephObjectStore.", "Platform", platform)
		return nil
	}

	cephObjectStores, err := r.newCephObjectStoreInstances(instance)
	if err != nil {
		return err
	}
	err = r.createCephObjectStores(cephObjectStores, instance)
	if err != nil {
		r.Log.Error(err, "Unable to create CephObjectStores for StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		return err
	}

	return nil
}

// ensureDeleted deletes the CephObjectStores owned by the StorageCluster
func (obj *ocsCephObjectStores) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	foundCephObjectStore := &cephv1.CephObjectStore{}
	cephObjectStores, err := r.newCephObjectStoreInstances(sc)
	if err != nil {
		return err
	}

	for _, cephObjectStore := range cephObjectStores {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStore not found.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrieve CephObjectStore %v: %v", cephObjectStore.Name, err)
		}

		if cephObjectStore.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Client.Delete(context.TODO(), foundCephObjectStore)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephObjectStore.", klog.KRef(foundCephObjectStore.Namespace, foundCephObjectStore.Name))
				return fmt.Errorf("uninstall: Failed to delete CephObjectStore %v: %v", foundCephObjectStore.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephObjectStore is deleted.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				continue
			}
		}
		r.Log.Error(err, "Uninstall: Waiting for CephObjectStore to be deleted.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
		return fmt.Errorf("uninstall: Waiting for CephObjectStore %v to be deleted", cephObjectStore.Name)

	}
	return nil
}

// createCephObjectStore creates CephObjectStore in the desired state
func (r *StorageClusterReconciler) createCephObjectStores(cephObjectStores []*cephv1.CephObjectStore, instance *ocsv1.StorageCluster) error {
	for _, cephObjectStore := range cephObjectStores {
		existing := cephv1.CephObjectStore{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)
		switch {
		case err == nil:
			reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				err := fmt.Errorf("failed to restore CephObjectStore object %s because it is marked for deletion", existing.Name)
				r.Log.Info("Failed to restore CephObjectStore because it is marked for deletion.", "CephObjectStore", klog.KRef(existing.Namespace, existing.Name))
				return err
			}

			r.Log.Info("Restoring original CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStore.ObjectMeta.OwnerReferences
			cephObjectStore.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephObjectStore)
			if err != nil {
				r.Log.Error(err, "Failed to update CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
			err = r.Client.Create(context.TODO(), cephObjectStore)
			if err != nil {
				r.Log.Error(err, "Failed to create CephObjectStore.", "CephObjectStore", klog.KRef(cephObjectStore.Namespace, cephObjectStore.Name))
				return err
			}
		}
	}
	return nil
}

// newCephObjectStoreInstances returns the cephObjectStore instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephObjectStoreInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephObjectStore, error) {
	gatewayInstances := initData.Spec.ManagedResources.CephObjectStores.GatewayInstances
	if gatewayInstances == 0 {
		gatewayInstances = getCephObjectStoreGatewayInstances(initData)
	}
	ret := []*cephv1.CephObjectStore{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				PreservePoolsOnDelete: false,
				DataPool: cephv1.PoolSpec{
					FailureDomain: initData.Status.FailureDomain,
					Replicated:    generateCephReplicatedSpec(initData, "data"),
				},
				MetadataPool: cephv1.PoolSpec{
					FailureDomain: initData.Status.FailureDomain,
					Replicated:    generateCephReplicatedSpec(initData, "metadata"),
				},
				Gateway: cephv1.GatewaySpec{
					Port:      80,
					Instances: gatewayInstances,
					Placement: getPlacement(initData, "rgw"),
					Resources: defaults.GetDaemonResources("rgw", initData.Spec.Resources),
					// set PriorityClassName for the rgw pods
					PriorityClassName: openshiftUserCritical,
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Failed to set ControllerReference for CephObjectStore.", "CephObjectStore", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}
	return ret, nil
}
