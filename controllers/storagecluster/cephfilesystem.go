package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephFilesystems struct{}

// newCephFilesystemInstances returns the cephFilesystem instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephFilesystemInstances(initStorageCluster *ocsv1.StorageCluster) ([]*cephv1.CephFilesystem, error) {
	ret := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephFilesystem(initStorageCluster),
			Namespace: initStorageCluster.Namespace,
		},
		Spec: cephv1.FilesystemSpec{
			MetadataPool: cephv1.NamedPoolSpec{
				PoolSpec: cephv1.PoolSpec{
					Replicated:    generateCephReplicatedSpec(initStorageCluster, "metadata"),
					FailureDomain: initStorageCluster.Status.FailureDomain,
				}},
			MetadataServer: cephv1.MetadataServerSpec{
				ActiveCount:   int32(getActiveMetadataServers(initStorageCluster)),
				ActiveStandby: true,
				Placement:     getPlacement(initStorageCluster, "mds"),
				Resources:     defaults.GetProfileDaemonResources("mds", initStorageCluster),
				// set PriorityClassName for the MDS pods
				PriorityClassName: openshiftUserCritical,
				Labels:            cephv1.Labels{defaults.ODFResourceProfileKey: initStorageCluster.Spec.ResourceProfile},
			},
		},
	}

	// Use the specified poolSpec, if it is unset then the default poolSpec will be used
	ret.Spec.DataPools = []cephv1.NamedPoolSpec{
		{
			PoolSpec: initStorageCluster.Spec.ManagedResources.CephFilesystems.DataPoolSpec,
		},
	}

	// Append additional pools from specified additional data pools
	ret.Spec.DataPools = append(ret.Spec.DataPools, initStorageCluster.Spec.ManagedResources.CephFilesystems.AdditionalDataPools...)

	// Iterate over each pool and set default values if necessary
	defaultPoolSpec := generateDefaultPoolSpec(initStorageCluster)
	for i := range ret.Spec.DataPools {
		pool := &ret.Spec.DataPools[i]
		// Set default device class if not specified
		if pool.PoolSpec.DeviceClass == "" {
			pool.PoolSpec.DeviceClass = defaultPoolSpec.DeviceClass
		}
		// Set EnableCrushUpdates to always be true
		pool.PoolSpec.EnableCrushUpdates = true
		// Set default replication settings if not specified
		if pool.PoolSpec.Replicated.Size == 0 {
			pool.PoolSpec.Replicated.Size = defaultPoolSpec.Replicated.Size
		}
		if pool.PoolSpec.Replicated.ReplicasPerFailureDomain == 0 {
			pool.PoolSpec.Replicated.ReplicasPerFailureDomain = defaultPoolSpec.Replicated.ReplicasPerFailureDomain
		}
		if pool.PoolSpec.Replicated.TargetSizeRatio == 0 {
			pool.PoolSpec.Replicated.TargetSizeRatio = defaultPoolSpec.Replicated.TargetSizeRatio
		}
		// Set default failure domain if not specified
		if pool.PoolSpec.FailureDomain == "" {
			pool.PoolSpec.FailureDomain = defaultPoolSpec.FailureDomain
		}
	}

	// set device class for metadata pool from the default data pool
	ret.Spec.MetadataPool.DeviceClass = ret.Spec.DataPools[0].PoolSpec.DeviceClass
	ret.Spec.MetadataPool.EnableCrushUpdates = true

	err := controllerutil.SetControllerReference(initStorageCluster, ret, r.Scheme)
	if err != nil {
		r.Log.Error(err, "Unable to set Controller Reference for CephFileSystem.", "CephFileSystem", klog.KRef(ret.Namespace, ret.Name))
		return nil, err
	}

	return []*cephv1.CephFilesystem{ret}, nil
}

// ensureCreated ensures that cephFilesystem resources exist in the desired
// state.
func (obj *ocsCephFilesystems) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	cephFilesystems, err := r.newCephFilesystemInstances(instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, cephFilesystem := range cephFilesystems {
		existing := cephv1.CephFilesystem{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: cephFilesystem.Namespace}, &existing)
		switch {
		case err == nil:
			if reconcileStrategy == ReconcileStrategyInit {
				return reconcile.Result{}, nil
			}
			if existing.DeletionTimestamp != nil {
				r.Log.Info("Unable to restore CephFileSystem because it is marked for deletion.", "CephFileSystem", klog.KRef(existing.Namespace, existing.Name))
				return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info("Restoring original CephFilesystem.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
			existing.ObjectMeta.OwnerReferences = cephFilesystem.ObjectMeta.OwnerReferences
			existing.Spec = cephFilesystem.Spec
			err = r.Client.Update(context.TODO(), &existing)
			if err != nil {
				r.Log.Error(err, "Unable to update CephFileSystem.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
				return reconcile.Result{}, err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephFileSystem.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
			err = r.Client.Create(context.TODO(), cephFilesystem)
			if err != nil {
				r.Log.Error(err, "Unable to create CephFileSystem.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephFilesystems owned by the StorageCluster
func (obj *ocsCephFilesystems) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	foundCephFilesystem := &cephv1.CephFilesystem{}
	cephFilesystems, err := r.newCephFilesystemInstances(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, cephFilesystem := range cephFilesystems {
		err := r.Client.Get(r.ctx, types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephFileSystem not found.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve CephFileSystem.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve CephFileSystem %v: %v", cephFilesystem.Name, err)
		}

		if cephFilesystem.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting cephFilesystem.", "CephFileSystem", klog.KRef(foundCephFilesystem.Namespace, foundCephFilesystem.Name))
			err = r.Client.Delete(r.ctx, foundCephFilesystem)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephFileSystem.", "CephFileSystem", klog.KRef(foundCephFilesystem.Namespace, foundCephFilesystem.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephFileSystem %v: %v", foundCephFilesystem.Name, err)
			}
		}

		err = r.Client.Get(r.ctx, types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephFilesystem is deleted.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
				continue
			}
		}
		r.Log.Error(err, "Uninstall: Waiting for CephFileSystem to be deleted.", "CephFileSystem", klog.KRef(cephFilesystem.Namespace, cephFilesystem.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephFileSystem %v to be deleted", cephFilesystem.Name)

	}
	return reconcile.Result{}, nil
}

func getActiveMetadataServers(sc *ocsv1.StorageCluster) int {
	activeMds := sc.Spec.ManagedResources.CephFilesystems.ActiveMetadataServers
	if activeMds != 0 {
		return activeMds
	}

	return defaults.CephFSActiveMetadataServers
}

// Define a function to generate default pool specifications
func generateDefaultPoolSpec(sc *ocsv1.StorageCluster) cephv1.PoolSpec {
	return cephv1.PoolSpec{
		DeviceClass:        sc.Status.DefaultCephDeviceClass,
		EnableCrushUpdates: true,
		Replicated:         generateCephReplicatedSpec(sc, "data"),
		FailureDomain:      sc.Status.FailureDomain,
	}
}
