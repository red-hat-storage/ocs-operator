package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
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
func (r *StorageClusterReconciler) newCephFilesystemInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephFilesystem, error) {
	ret := &cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephFilesystem(initData),
			Namespace: initData.Namespace,
		},
		Spec: cephv1.FilesystemSpec{
			MetadataPool: cephv1.PoolSpec{
				Replicated:    generateCephReplicatedSpec(initData, "metadata"),
				FailureDomain: initData.Status.FailureDomain,
			},
			MetadataServer: cephv1.MetadataServerSpec{
				ActiveCount:   1,
				ActiveStandby: true,
				Placement:     getPlacement(initData, "mds"),
				Resources:     defaults.GetDaemonResources("mds", initData.Spec.Resources),
				// set PriorityClassName for the MDS pods
				PriorityClassName: openshiftUserCritical,
			},
		},
	}

	if initData.Spec.StorageProfiles == nil {
		// standalone deployment will not have storageProfile, we need to
		// define default dataPool, if storageProfile is set this will be
		// overridden.
		ret.Spec.DataPools = []cephv1.NamedPoolSpec{
			{
				PoolSpec: cephv1.PoolSpec{
					DeviceClass:   generateDeviceClass(initData),
					Replicated:    generateCephReplicatedSpec(initData, "data"),
					FailureDomain: initData.Status.FailureDomain,
				},
			},
		}
	} else {
		// set deviceClass and parameters from storageProfile
		for i := range initData.Spec.StorageProfiles {
			deviceClass := initData.Spec.StorageProfiles[i].DeviceClass
			parameters := initData.Spec.StorageProfiles[i].SharedFilesystemConfiguration.Parameters
			ret.Spec.DataPools = append(ret.Spec.DataPools, cephv1.NamedPoolSpec{
				Name: deviceClass,
				PoolSpec: cephv1.PoolSpec{
					Replicated:    generateCephReplicatedSpec(initData, "data"),
					DeviceClass:   deviceClass,
					Parameters:    parameters,
					FailureDomain: initData.Status.FailureDomain,
				},
			})
		}
	}

	err := controllerutil.SetControllerReference(initData, ret, r.Scheme)
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
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
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
			err = r.Client.Delete(context.TODO(), foundCephFilesystem)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephFileSystem.", "CephFileSystem", klog.KRef(foundCephFilesystem.Namespace, foundCephFilesystem.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephFileSystem %v: %v", foundCephFilesystem.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
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
