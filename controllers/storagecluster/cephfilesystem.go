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
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephFilesystems struct{}

const defaultSubvolumeGroupName = "csi"

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
				PoolSpec: initStorageCluster.Spec.ManagedResources.CephFilesystems.MetadataPoolSpec, // Pass the poolSpec from the storageCluster CR
			},
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
	for i := range ret.Spec.DataPools {
		poolSpec := &ret.Spec.DataPools[i].PoolSpec
		// Set default values in the poolSpec as necessary
		setDefaultDataPoolSpec(poolSpec, initStorageCluster)
	}

	// Set default values in the metadata pool spec as necessary
	setDefaultDataPoolSpec(&ret.Spec.MetadataPool.PoolSpec, initStorageCluster)

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
		// create default csi subvolumegroup for the filesystem
		// skip for the ocs provider mode
		if !instance.Spec.AllowRemoteStorageConsumers {
			err = r.createDefaultSubvolumeGroup(cephFilesystem.Name, cephFilesystem.Namespace, cephFilesystem.ObjectMeta.OwnerReferences)
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClusterReconciler) createDefaultSubvolumeGroup(filesystemName, filesystemNamespace string, ownerReferences []metav1.OwnerReference) error {

	existingsvg := &cephv1.CephFilesystemSubVolumeGroup{}
	svgName := generateNameForCephSubvolumeGroup(filesystemName)
	err := r.Client.Get(r.ctx, types.NamespacedName{Name: svgName, Namespace: filesystemNamespace}, existingsvg)
	if err == nil {
		if existingsvg.DeletionTimestamp != nil {
			r.Log.Info("Unable to restore subvolumegroup because it is marked for deletion.", "subvolumegroup", klog.KRef(filesystemNamespace, existingsvg.Name))
			return fmt.Errorf("failed to restore subvolumegroup %s because it is marked for deletion", existingsvg.Name)
		}
	}

	cephFilesystemSubVolumeGroup := &cephv1.CephFilesystemSubVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:            svgName,
			Namespace:       filesystemNamespace,
			OwnerReferences: ownerReferences,
		},
	}

	// Default value of "distributed" option for pinning in the CephFilesystemSubVolumeGroup CR
	defaultPinningValue := 1
	mutateFn := func() error {
		cephFilesystemSubVolumeGroup.Spec = cephv1.CephFilesystemSubVolumeGroupSpec{
			Name:           defaultSubvolumeGroupName,
			FilesystemName: filesystemName,
			Pinning: cephv1.CephFilesystemSubVolumeGroupSpecPinning{
				Distributed: &defaultPinningValue,
			},
		}
		return nil
	}
	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, cephFilesystemSubVolumeGroup, mutateFn)
	if err != nil {
		r.Log.Error(err, "Could not create/update default csi cephFilesystemSubVolumeGroup.", "cephFilesystemSubVolumeGroup", klog.KRef(cephFilesystemSubVolumeGroup.Namespace, cephFilesystemSubVolumeGroup.Name))
		return err
	}
	return nil
}

func (r *StorageClusterReconciler) deleteDefaultSubvolumeGroup(filesystemName, filesystemNamespace string) error {
	existingsvg := &cephv1.CephFilesystemSubVolumeGroup{}
	svgName := generateNameForCephSubvolumeGroup(filesystemName)
	err := r.Client.Get(r.ctx, types.NamespacedName{Name: svgName, Namespace: filesystemNamespace}, existingsvg)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: csi subvolumegroup not found.", "Subvolumegroup", klog.KRef(filesystemNamespace, svgName))
			return nil
		}
		r.Log.Error(err, "Uninstall: Unable to retrieve subvolumegroup.", "subvolumegroup", klog.KRef(filesystemNamespace, svgName))
		return fmt.Errorf("uninstall: Unable to retrieve csi subvolumegroup : %v", err)
	}

	if existingsvg.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting subvolumegroup.", "subvolumegroup", klog.KRef(filesystemNamespace, existingsvg.Name))
		err = r.Client.Delete(r.ctx, existingsvg)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete subvolumegroup.", "subvolumegroup", klog.KRef(filesystemNamespace, existingsvg.Name))
			return fmt.Errorf("uninstall: Failed to delete subvolumegroup %v: %v", existingsvg.Name, err)
		}
	}

	err = r.Client.Get(r.ctx, types.NamespacedName{Name: svgName, Namespace: filesystemNamespace}, existingsvg)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: subvolumegroup is deleted.", "subvolumegroup", klog.KRef(filesystemNamespace, existingsvg.Name))
			return nil
		}
	}
	r.Log.Error(err, "Uninstall: Waiting for subvolumegroup to be deleted.", "subvolumegroup", klog.KRef(filesystemNamespace, existingsvg.Name))
	return fmt.Errorf("uninstall: Waiting for subvolumegroup %v to be deleted", existingsvg.Name)
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

		// delete csi subvolume group for particular filesystem
		// skip for the ocs provider mode
		if !sc.Spec.AllowRemoteStorageConsumers {
			cephSVGName := generateNameForCephSubvolumeGroup(cephFilesystem.Name)
			err = r.deleteDefaultSubvolumeGroup(cephFilesystem.Name, cephFilesystem.Namespace)
			if err != nil {
				r.Log.Error(err, "Uninstall: unable to delete subvolumegroup", "subvolumegroup", klog.KRef(cephFilesystem.Namespace, cephSVGName))
				return reconcile.Result{}, err
			}
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
