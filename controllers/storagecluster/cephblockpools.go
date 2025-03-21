package storagecluster

import (
	"fmt"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephBlockPools struct{}

func (o *ocsCephBlockPools) deleteCephBlockPool(r *StorageClusterReconciler, cephBlockPool *cephv1.CephBlockPool) (reconcile.Result, error) {
	// if deletion timestamp is set, wait till block pool is deleted
	if cephBlockPool.DeletionTimestamp != nil {
		r.Log.Info("Uninstall: Waiting for CephBlockPool to be deleted.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephBlockPool %v to be deleted", cephBlockPool.Name)
	}

	// delete
	r.Log.Info("Uninstall: Deleting CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
	if err := r.Client.Delete(r.ctx, cephBlockPool); err != nil {
		r.Log.Error(err, "Uninstall: Failed to delete CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephBlockPool %v: %v", cephBlockPool.Name, err)
	}
	// Requeue as we need to wait till cephBlockPool is deleted
	return reconcile.Result{Requeue: true}, nil
}

func (o *ocsCephBlockPools) reconcileCephBlockPool(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	cephBlockPool := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      GenerateNameForCephBlockPool(storageCluster),
			Namespace: storageCluster.Namespace,
		},
	}

	// Get to see if it already exists
	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	// storageCluster is marked for deletion - delete the block pool
	if storageCluster.GetDeletionTimestamp() != nil {
		// if found, delete the block pool
		if !errors.IsNotFound(err) {
			return o.deleteCephBlockPool(r, cephBlockPool)
		}
		return reconcile.Result{}, nil
	}

	// If found and reconcileStrategy is init we skip
	if !errors.IsNotFound(err) && ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy) == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
		// Pass the poolSpec from the storageCluster CR

		existingPoolSpec := cephBlockPool.Spec.PoolSpec
		if storageCluster.Spec.ManagedResources.CephBlockPools.PoolSpec != nil {
			cephBlockPool.Spec.PoolSpec = *storageCluster.Spec.ManagedResources.CephBlockPools.PoolSpec
		}

		// Set default values in the poolSpec as necessary
		setDefaultDataPoolSpec(&cephBlockPool.Spec.PoolSpec, storageCluster)
		cephBlockPool.Spec.PoolSpec.EnableRBDStats = true

		// The bulk flag is set to true only during new pool creation, as setting it on existing pools can cause data movement.
		if cephBlockPool.CreationTimestamp.IsZero() {
			setBulkFlagParameter(&cephBlockPool.Spec.PoolSpec.Parameters)
		}

		// Ensures the bulk flag set during new pool creation is not removed during updates.
		preserveBulkFlagParameter(existingPoolSpec.Parameters, &cephBlockPool.Spec.PoolSpec.Parameters)

		return controllerutil.SetControllerReference(storageCluster, cephBlockPool, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (o *ocsCephBlockPools) reconcileMgrCephBlockPool(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	// This name is used by UI to hide this pool from the list of CephBlockPools
	builtinMgrPoolName := "builtin-mgr"

	cephBlockPool := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      builtinMgrPoolName,
			Namespace: storageCluster.Namespace,
		},
	}

	// Get to see if it already exists
	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	// storageCluster is marked for deletion - delete the block pool
	if storageCluster.GetDeletionTimestamp() != nil {
		// if found, delete the block pool
		if !errors.IsNotFound(err) {
			return o.deleteCephBlockPool(r, cephBlockPool)
		}
		return reconcile.Result{}, nil
	}

	// If found and reconcileStrategy is init we skip
	if !errors.IsNotFound(err) && ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy) == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
		cephBlockPool.Spec.Name = ".mgr"
		setDefaultMetadataPoolSpec(&cephBlockPool.Spec.PoolSpec, storageCluster)
		util.AddLabel(cephBlockPool, util.ForbidMirroringLabel, "true")

		return controllerutil.SetControllerReference(storageCluster, cephBlockPool, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (o *ocsCephBlockPools) reconcileNFSCephBlockPool(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if storageCluster.Spec.NFS == nil || !storageCluster.Spec.NFS.Enable {
		return reconcile.Result{}, nil
	}

	cephBlockPool := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephNFSBlockPool(storageCluster),
			Namespace: storageCluster.Namespace,
		},
	}

	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	// storageCluster is marked for deletion - delete the block pool
	if storageCluster.GetDeletionTimestamp() != nil {
		// if found, delete the block pool
		if !errors.IsNotFound(err) {
			return o.deleteCephBlockPool(r, cephBlockPool)
		}
		return reconcile.Result{}, nil
	}

	if !errors.IsNotFound(err) && ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy) == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
		cephBlockPool.Spec.Name = ".nfs"
		setDefaultMetadataPoolSpec(&cephBlockPool.Spec.PoolSpec, storageCluster)
		cephBlockPool.Spec.PoolSpec.EnableRBDStats = true
		util.AddLabel(cephBlockPool, util.ForbidMirroringLabel, "true")

		return controllerutil.SetControllerReference(storageCluster, cephBlockPool, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (o *ocsCephBlockPools) reconcileNonResilientCephBlockPool(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if !storageCluster.Spec.ManagedResources.CephNonResilientPools.Enable {
		return reconcile.Result{}, nil
	}

	for _, failureDomainValue := range storageCluster.Status.FailureDomainValues {

		cephBlockPool := &cephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForNonResilientCephBlockPool(storageCluster, failureDomainValue),
				Namespace: storageCluster.Namespace,
			},
		}

		err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
		if client.IgnoreNotFound(err) != nil {
			return reconcile.Result{}, err
		}

		// storageCluster is marked for deletion - delete the block pool
		if storageCluster.GetDeletionTimestamp() != nil {
			// if found, delete the block pool
			if !errors.IsNotFound(err) {
				return o.deleteCephBlockPool(r, cephBlockPool)
			}
			return reconcile.Result{}, nil
		}

		if !errors.IsNotFound(err) && ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy) == ReconcileStrategyInit {
			return reconcile.Result{}, nil
		}

		_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, cephBlockPool, func() error {
			poolSpec := &cephBlockPool.Spec.PoolSpec
			poolSpec.DeviceClass = failureDomainValue
			poolSpec.EnableCrushUpdates = true
			poolSpec.FailureDomain = getFailureDomain(storageCluster)
			existingParameters := poolSpec.Parameters
			poolSpec.Parameters = storageCluster.Spec.ManagedResources.CephNonResilientPools.Parameters
			if poolSpec.Parameters == nil {
				poolSpec.Parameters = make(map[string]string)
			}

			// The bulk flag is set to true only during new pool creation, as setting it on existing pools can cause data movement.
			if cephBlockPool.CreationTimestamp.IsZero() {
				setBulkFlagParameter(&poolSpec.Parameters)
			}

			// Ensures the bulk flag set during new pool creation is not removed during updates.
			preserveBulkFlagParameter(existingParameters, &poolSpec.Parameters)

			// set the pg_num & pgp_num parameters only when bulk flag is not set or false
			if bulk, ok := poolSpec.Parameters["bulk"]; !ok {
				b, err := strconv.ParseBool(bulk)
				if err != nil || !b {
					if _, ok := poolSpec.Parameters["pg_num"]; !ok {
						poolSpec.Parameters["pg_num"] = "16"
					}
					if _, ok := poolSpec.Parameters["pgp_num"]; !ok {
						poolSpec.Parameters["pgp_num"] = "16"
					}
				}
			}

			poolSpec.Replicated = cephv1.ReplicatedSpec{
				Size:                   1,
				RequireSafeReplicaSize: false,
			}
			poolSpec.EnableRBDStats = true

			return controllerutil.SetControllerReference(storageCluster, cephBlockPool, r.Scheme)
		})
		if err != nil {
			r.Log.Error(err, "Failed to create/update CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

// ensureCreated ensures that cephBlockPool resources exist in the desired state.
func (o *ocsCephBlockPools) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	//Create cephBlockPool one by one
	if res, err := o.reconcileCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileMgrCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileNonResilientCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileNFSCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephBlockPools owned by the StorageCluster
func (o *ocsCephBlockPools) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	//Create cephBlockPool one by one
	if res, err := o.reconcileCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileMgrCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileNonResilientCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileNFSCephBlockPool(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}
