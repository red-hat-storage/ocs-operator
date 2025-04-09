package storagecluster

import (
	"fmt"
	"reflect"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsGroupSnapshotClass struct{}

type GroupSnapshotClassConfiguration struct {
	groupSnapshotClass *groupsnapapi.VolumeGroupSnapshotClass
	reconcileStrategy  ReconcileStrategy
}

func (r *StorageClusterReconciler) createGroupSnapshotClasses(vsccs []GroupSnapshotClassConfiguration) error {

	for _, vscc := range vsccs {
		if vscc.reconcileStrategy == ReconcileStrategyIgnore {
			continue
		}

		vsc := vscc.groupSnapshotClass
		existing := &groupsnapapi.VolumeGroupSnapshotClass{}
		err := r.Client.Get(r.ctx, types.NamespacedName{Name: vsc.Name, Namespace: vsc.Namespace}, existing)
		if err != nil {
			if errors.IsNotFound(err) {
				// Since the SnapshotClass is not found, we will create a new one
				r.Log.Info("Creating GroupSnapshotClass.", "GroupSnapshotClass", klog.KRef("", vsc.Name))
				err = r.Client.Create(r.ctx, vsc)
				if err != nil {
					r.Log.Error(err, "Failed to create GroupSnapshotClass.", "GroupSnapshotClass", klog.KRef("", vsc.Name))
					return err
				}
				// no error, continue with the next iteration
				continue
			}

			r.Log.Error(err, "Failed to 'Get' GroupSnapshotClass.", "GroupSnapshotClass", klog.KRef("", vsc.Name))
			return err
		}
		if vscc.reconcileStrategy == ReconcileStrategyInit {
			return nil
		}
		if existing.DeletionTimestamp != nil {
			return fmt.Errorf("failed to restore GroupSnapshotClass %q because it is marked for deletion", existing.Name)
		}
		// if there is a mismatch in the parameters of existing vs created resources,
		if !reflect.DeepEqual(vsc.Parameters, existing.Parameters) {
			// we have to update the existing SnapshotClass
			r.Log.Info("GroupSnapshotClass needs to be updated", "GroupSnapshotClass", klog.KRef("", existing.Name))
			existing.ObjectMeta.OwnerReferences = vsc.ObjectMeta.OwnerReferences
			vsc.ObjectMeta = existing.ObjectMeta
			if err := r.Client.Update(r.ctx, vsc); err != nil {
				r.Log.Error(err, "GroupSnapshotClass updation failed.", "GroupSnapshotClass", klog.KRef("", existing.Name))
				return err
			}
		}
	}
	return nil
}

func (obj *ocsGroupSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if !r.AvailableCrds[VolumeGroupSnapshotClassCrdName] {
		r.Log.Info("VolumeGroupSnapshotClass CRD is not available")
		return reconcile.Result{}, nil
	}

	rbdClusterID, rbdProvisionerSecret, err := r.getClusterIDAndSecretName(instance, util.RbdSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}
	rbdGroupSnapshotClass := GroupSnapshotClassConfiguration{
		groupSnapshotClass: util.NewDefaultRbdGroupSnapshotClass(
			rbdClusterID,
			rbdProvisionerSecret,
			instance.Namespace,
			util.GenerateNameForCephBlockPool(instance.Name),
			"",
		),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy),
	}
	rbdGroupSnapshotClass.groupSnapshotClass.Name = util.GenerateNameForGroupSnapshotClass(instance, util.RbdGroupSnapshotter)

	cephfsClusterID, cephfsProvisionerSecret, err := r.getClusterIDAndSecretName(instance, util.CephfsSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}
	cephFsGroupSnapshotClass := GroupSnapshotClassConfiguration{
		groupSnapshotClass: util.NewDefaultCephFsGroupSnapshotClass(
			cephfsClusterID,
			cephfsProvisionerSecret,
			instance.Namespace,
			util.GenerateNameForCephFilesystem(instance.Name),
			"",
		),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
	}
	cephFsGroupSnapshotClass.groupSnapshotClass.Name = util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter)

	volumeGroupSnapshotClasses := []GroupSnapshotClassConfiguration{
		rbdGroupSnapshotClass,
		cephFsGroupSnapshotClass,
	}

	err = r.createGroupSnapshotClasses(volumeGroupSnapshotClasses)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (obj *ocsGroupSnapshotClass) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if !r.AvailableCrds[VolumeGroupSnapshotClassCrdName] {
		r.Log.Info("VolumeGroupSnapshotClass CRD doesn't exist")
		return reconcile.Result{}, nil
	}

	names := []string{
		util.GenerateNameForGroupSnapshotClass(instance, util.RbdGroupSnapshotter),
		util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter),
	}
	for _, name := range names {
		vgsc := &groupsnapapi.VolumeGroupSnapshotClass{}
		vgsc.Name = name
		vgsc.Namespace = instance.Namespace
		err := r.Client.Delete(r.ctx, vgsc)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: GroupSnapshotClass not found, nothing to do.", "GroupSnapshotClass", klog.KRef("", vgsc.Name))
			} else {
				r.Log.Error(err, "Uninstall: Error while deleting GroupSnapshotClass.", "GroupSnapshotClass", klog.KRef("", vgsc.Name))
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}
