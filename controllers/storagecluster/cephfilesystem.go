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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsCephFilesystems struct{}

// newCephFilesystemInstances returns the cephFilesystem instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephFilesystemInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephFilesystem, error) {
	ret := []*cephv1.CephFilesystem{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephFilesystem(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.FilesystemSpec{
				MetadataPool: cephv1.PoolSpec{
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
					FailureDomain: initData.Status.FailureDomain,
				},
				DataPools: []cephv1.PoolSpec{
					{
						Replicated: cephv1.ReplicatedSpec{
							Size:            3,
							TargetSizeRatio: .49,
						},
						FailureDomain: initData.Status.FailureDomain,
					},
				},
				MetadataServer: cephv1.MetadataServerSpec{
					ActiveCount:   1,
					ActiveStandby: true,
					Placement:     getPlacement(initData, "mds"),
					Resources:     defaults.GetDaemonResources("mds", initData.Spec.Resources),
				},
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

// ensureCreated ensures that cephFilesystem resources exist in the desired
// state.
func (obj *ocsCephFilesystems) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	cephFilesystems, err := r.newCephFilesystemInstances(instance)
	if err != nil {
		return err
	}
	for _, cephFilesystem := range cephFilesystems {
		existing := cephv1.CephFilesystem{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: cephFilesystem.Namespace}, &existing)
		switch {
		case err == nil:
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				r.Log.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info(fmt.Sprintf("Restoring original cephFilesystem %s", cephFilesystem.Name))
			existing.ObjectMeta.OwnerReferences = cephFilesystem.ObjectMeta.OwnerReferences
			cephFilesystem.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephFilesystem)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info(fmt.Sprintf("Creating cephFilesystem %s", cephFilesystem.Name))
			err = r.Client.Create(context.TODO(), cephFilesystem)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

// ensureDeleted deletes the CephFilesystems owned by the StorageCluster
func (obj *ocsCephFilesystems) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	foundCephFilesystem := &cephv1.CephFilesystem{}
	cephFilesystems, err := r.newCephFilesystemInstances(sc)
	if err != nil {
		return err
	}

	for _, cephFilesystem := range cephFilesystems {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephFilesystem not found", "CephFilesystem Name", cephFilesystem.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrieve cephFilesystem %v: %v", cephFilesystem.Name, err)
		}

		if cephFilesystem.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting cephFilesystem", "CephFilesystem Name", cephFilesystem.Name)
			err = r.Client.Delete(context.TODO(), foundCephFilesystem)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephFilesystem %v: %v", foundCephFilesystem.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephFilesystem is deleted", "CephFilesystem Name", cephFilesystem.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephFilesystem %v to be deleted", cephFilesystem.Name)

	}
	return nil
}
