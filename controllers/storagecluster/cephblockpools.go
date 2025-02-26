package storagecluster

import (
	"fmt"

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

// ensures that peer cluster secret exists and adds it to CephBlockPool
func (o *ocsCephBlockPools) addPeerSecretsToCephBlockPool(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster, poolName, poolNamespace string) *cephv1.MirroringPeerSpec {
	mirroringPeerSpec := &cephv1.MirroringPeerSpec{}
	secretNames := []string{}

	if len(storageCluster.Spec.Mirroring.PeerSecretNames) == 0 {
		err := fmt.Errorf("mirroring is enabled but peerSecretNames is not provided")
		r.Log.Error(err, "Unable to add cluster peer token to CephBlockPool.", "CephBlockPool", klog.KRef(poolNamespace, poolName))
		return mirroringPeerSpec
	}
	for _, secretName := range storageCluster.Spec.Mirroring.PeerSecretNames {
		_, err := r.retrieveSecret(secretName, storageCluster)
		if err != nil {
			r.Log.Error(err, "Peer cluster token could not be retrieved using secretname.", "CephBlockPool", klog.KRef(poolNamespace, poolName))
			return mirroringPeerSpec
		}
		secretNames = append(secretNames, secretName)
	}

	mirroringPeerSpec.SecretNames = secretNames
	return mirroringPeerSpec
}

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
			Name:      generateNameForCephBlockPool(storageCluster),
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

		cephBlockPool.Spec.PoolSpec = storageCluster.Spec.ManagedResources.CephBlockPools.PoolSpec

		// Set default values in the poolSpec as necessary
		setDefaultDataPoolSpec(&cephBlockPool.Spec.PoolSpec, storageCluster)
		cephBlockPool.Spec.PoolSpec.EnableRBDStats = true

		// Since provider mode handles mirroring, we only need to handle for internal mode
		if storageCluster.Annotations["ocs.openshift.io/deployment-mode"] != "provider" {
			if storageCluster.Spec.Mirroring != nil && storageCluster.Spec.Mirroring.Enabled {
				cephBlockPool.Spec.PoolSpec.Mirroring.Enabled = true
				cephBlockPool.Spec.PoolSpec.Mirroring.Mode = "image"
				cephBlockPool.Spec.PoolSpec.Mirroring.Peers = o.addPeerSecretsToCephBlockPool(r, storageCluster, cephBlockPool.Name, cephBlockPool.Namespace)
			} else {
				// If mirroring is not enabled or is nil, disable it. This is to ensure that the pool mirroring does not remain enabled during further reconciliations
				cephBlockPool.Spec.PoolSpec.Mirroring = cephv1.MirroringSpec{Enabled: false}
			}
		}
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
		cephBlockPool.Spec.PoolSpec.DeviceClass = storageCluster.Status.DefaultCephDeviceClass
		cephBlockPool.Spec.PoolSpec.EnableCrushUpdates = true
		cephBlockPool.Spec.PoolSpec.FailureDomain = getFailureDomain(storageCluster)
		cephBlockPool.Spec.PoolSpec.Replicated = generateCephReplicatedSpec(storageCluster, poolTypeMetadata)
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
		cephBlockPool.Spec.PoolSpec.DeviceClass = storageCluster.Status.DefaultCephDeviceClass
		cephBlockPool.Spec.EnableCrushUpdates = true
		cephBlockPool.Spec.PoolSpec.FailureDomain = getFailureDomain(storageCluster)
		cephBlockPool.Spec.PoolSpec.Replicated = generateCephReplicatedSpec(storageCluster, poolTypeMetadata)
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
			poolSpec.Parameters = storageCluster.Spec.ManagedResources.CephNonResilientPools.Parameters
			if poolSpec.Parameters == nil {
				poolSpec.Parameters = make(map[string]string)
			}
			if _, ok := poolSpec.Parameters["pg_num"]; !ok {
				poolSpec.Parameters["pg_num"] = "16"
			}
			if _, ok := poolSpec.Parameters["pgp_num"]; !ok {
				poolSpec.Parameters["pgp_num"] = "16"
			}
			poolSpec.Replicated = cephv1.ReplicatedSpec{
				Size:                   1,
				RequireSafeReplicaSize: false,
			}
			poolSpec.EnableRBDStats = true

			// Since provider mode handles mirroring, we only need to handle for internal mode
			if storageCluster.Annotations["ocs.openshift.io/deployment-mode"] != "provider" {
				if storageCluster.Spec.Mirroring != nil && storageCluster.Spec.Mirroring.Enabled {
					cephBlockPool.Spec.PoolSpec.Mirroring.Enabled = true
					cephBlockPool.Spec.PoolSpec.Mirroring.Mode = "image"
					cephBlockPool.Spec.PoolSpec.Mirroring.Peers = o.addPeerSecretsToCephBlockPool(r, storageCluster, cephBlockPool.Name, cephBlockPool.Namespace)
				} else {
					// If mirroring is not enabled or is nil, disable it. This is to ensure that the pool mirroring does not remain enabled during further reconciliations
					cephBlockPool.Spec.PoolSpec.Mirroring = cephv1.MirroringSpec{Enabled: false}
				}
			}
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
