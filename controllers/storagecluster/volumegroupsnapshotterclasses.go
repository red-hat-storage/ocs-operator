package storagecluster

import (
	"encoding/json"
	"fmt"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

		vgsc := &groupsnapapi.VolumeGroupSnapshotClass{}
		vgsc.Name = vscc.groupSnapshotClass.Name
		err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(vgsc), vgsc)
		if client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Failed to 'Get' GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(vgsc))
			return err
		}

		// If found and reconcileStrategy is init we skip
		if !errors.IsNotFound(err) && vscc.reconcileStrategy == ReconcileStrategyInit {
			continue
		}

		if vgsc.DeletionTimestamp != nil {
			return fmt.Errorf("failed to restore GroupSnapshotClass %q because it is marked for deletion", vgsc.Name)
		}

		_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, vgsc, func() error {

			// Unmarshal follows merge semantics, that means that we don't need to worry about overriding the status,
			// or any metadata fields. There is an exception when it comes to creationTimestamp which gets serialized into
			// default value.
			desiredBytes := util.JsonMustMarshal(vscc.groupSnapshotClass)
			creationTimestamp := vgsc.GetCreationTimestamp()
			if err := json.Unmarshal(desiredBytes, vgsc); err != nil {
				return fmt.Errorf("failed to unmarshal %s configuration response: %v", vgsc.GetName(), err)
			}
			vgsc.SetCreationTimestamp(creationTimestamp)
			return nil
		})
		if err != nil {
			r.Log.Error(err, "Failed to create or update GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(vgsc))
			return err
		}
	}
	return nil
}

func (obj *ocsGroupSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if val, _ := r.crdsBeingWatched.Load(VolumeGroupSnapshotClassCrdName); !val.(bool) {
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
	util.AddLabel(rbdGroupSnapshotClass.groupSnapshotClass, util.ExternalClassLabelKey, strconv.FormatBool(true))

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
	util.AddLabel(cephFsGroupSnapshotClass.groupSnapshotClass, util.ExternalClassLabelKey, strconv.FormatBool(true))

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
	if val, _ := r.crdsBeingWatched.Load(VolumeGroupSnapshotClassCrdName); !val.(bool) {
		r.Log.Info("VolumeGroupSnapshotClass CRD is not available")
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
