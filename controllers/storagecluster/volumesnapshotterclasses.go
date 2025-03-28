package storagecluster

import (
	"context"
	"fmt"
	"reflect"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsSnapshotClass struct{}

// SnapshotClassConfiguration provides configuration options for a SnapshotClass.
type SnapshotClassConfiguration struct {
	snapshotClass     *snapapi.VolumeSnapshotClass
	reconcileStrategy ReconcileStrategy
}

func (r *StorageClusterReconciler) createSnapshotClasses(vsccs []SnapshotClassConfiguration) error {

	for _, vscc := range vsccs {
		if vscc.reconcileStrategy == ReconcileStrategyIgnore {
			continue
		}

		vsc := vscc.snapshotClass
		existing := &snapapi.VolumeSnapshotClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: vsc.Name, Namespace: vsc.Namespace}, existing)
		if err != nil {
			if errors.IsNotFound(err) {
				// Since the SnapshotClass is not found, we will create a new one
				r.Log.Info("Creating SnapshotClass.", "SnapshotClass", klog.KRef(vsc.Namespace, vsc.Name))
				err = r.Client.Create(context.TODO(), vsc)
				if err != nil {
					r.Log.Error(err, "Failed to create SnapshotClass.", "SnapshotClass", klog.KRef(vsc.Namespace, vsc.Name))
					return err
				}
				// no error, continue with the next iteration
				continue
			}

			r.Log.Error(err, "Failed to 'Get' SnapshotClass.", "SnapshotClass", klog.KRef(vsc.Namespace, vsc.Name))
			return err
		}
		if vscc.reconcileStrategy == ReconcileStrategyInit {
			return nil
		}
		if existing.DeletionTimestamp != nil {
			return fmt.Errorf("failed to restore SnapshotClass %q because it is marked for deletion", existing.Name)
		}
		// if there is a mismatch in the parameters of existing vs created resources,
		if !reflect.DeepEqual(vsc.Parameters, existing.Parameters) {
			// we have to update the existing SnapshotClass
			r.Log.Info("SnapshotClass needs to be updated", "SnapshotClass", klog.KRef(existing.Namespace, existing.Name))
			existing.ObjectMeta.OwnerReferences = vsc.ObjectMeta.OwnerReferences
			vsc.ObjectMeta = existing.ObjectMeta
			if err := r.Client.Update(context.TODO(), vsc); err != nil {
				r.Log.Error(err, "SnapshotClass updation failed.", "SnapshotClass", klog.KRef(existing.Namespace, existing.Name))
				return err
			}
		}
	}
	return nil
}

// ensureCreated functions ensures that snpashotter classes are created
func (obj *ocsSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	rbdSnapClass := SnapshotClassConfiguration{
		snapshotClass:     util.NewDefaultRbdSnapshotClass(instance.Namespace, "rook-csi-rbd-provisioner", instance.Namespace, ""),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy),
	}
	rbdSnapClass.snapshotClass.Name = util.GenerateNameForSnapshotClass(instance.Name, util.RbdSnapshotter)
	cephFsSnapClass := SnapshotClassConfiguration{
		snapshotClass:     util.NewDefaultCephFsSnapshotClass(instance.Namespace, "rook-csi-cephfs-provisioner", instance.Namespace, ""),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
	}
	cephFsSnapClass.snapshotClass.Name = util.GenerateNameForSnapshotClass(instance.Name, util.CephfsSnapshotter)
	volumeSnapshotClasses := []SnapshotClassConfiguration{rbdSnapClass, cephFsSnapClass}

	err := r.createSnapshotClasses(volumeSnapshotClasses)
	if err != nil {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the SnapshotClasses that the ocs-operator created
func (obj *ocsSnapshotClass) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	names := []string{
		util.GenerateNameForGroupSnapshotClass(instance, util.RbdGroupSnapshotter),
		util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter),
	}
	for _, name := range names {
		vsc := &snapapi.VolumeSnapshotClass{}
		vsc.Name = name
		vsc.Namespace = instance.Namespace
		err := r.Client.Delete(r.ctx, vsc)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: SnapshotClass not found, nothing to do.", "SnapshotClass", klog.KRef("", vsc.Name))
			} else {
				r.Log.Error(err, "Uninstall: Error while deleting SnapshotClass.", "SnapshotClass", klog.KRef("", vsc.Name))
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}
