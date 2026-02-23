package storagecluster

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	// csiProvisionerSecretName is the default name of the csi provisioner secret name
	csiProvisionerSecretName = "rook-csi-%s-provisioner"
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

func (r *StorageClusterReconciler) getClusterIDAndSecretName(instance *ocsv1.StorageCluster, snapshotType util.SnapshotterType) (string, string, error) {
	clusterID := instance.Namespace
	secretName := fmt.Sprintf(csiProvisionerSecretName, snapshotType)

	if !instance.Spec.ExternalStorage.Enable {
		return clusterID, secretName, nil
	}

	data, ok := externalOCSResources[instance.UID]
	if !ok {
		err := fmt.Errorf("unable to retrieve external resource from externalOCSResources")
		log.Error(err, "unable to generate name for snapshot class secret for external mode")
		return "", "", err
	}
	// print the Secret name which contains the prefix as the rook-csi-rbd/cephfs-provisioner default secret name
	// for example if the secret name is rook-csi-rbd-node-rookStorage-provisioner it will check the prefix with rook-csi-rbd-node if it matches it will return that name
	for _, d := range data {
		if d.Kind == "Secret" {
			if strings.Contains(d.Name, fmt.Sprintf(csiProvisionerSecretName, snapshotType)) {
				secretName = d.Name
			}
		}

		if (d.Kind == "CephBlockPoolRadosNamespace") && (snapshotType == util.RbdSnapshotter) {
			radosNamespaceName = d.Data["radosNamespaceName"]
			// get the clusterID from the radosNamespace status
			radosNamespace := &cephv1.CephBlockPoolRadosNamespace{}
			err := r.Client.Get(context.TODO(), types.NamespacedName{Name: radosNamespaceName, Namespace: instance.Namespace}, radosNamespace)
			if err != nil {
				log.Error(err, "CephBlockPoolRadosNamespace not found", "CephBlockPoolRadosNamespace", klog.KRef(instance.Namespace, radosNamespaceName))
				return "", "", err
			}
			// get the clusterID from the radosNamespace status
			if radosNamespace.Status.Info["clusterID"] != "" {
				clusterID = radosNamespace.Status.Info["clusterID"]
			}
		}
	}

	return clusterID, secretName, nil

}

// ensureCreated functions ensures that snpashotter classes are created
func (obj *ocsSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	var fsid string
	if cephCluster, err := util.GetCephClusterInNamespace(r.ctx, r.Client, instance.Namespace); err != nil {
		return reconcile.Result{}, err
	} else if cephCluster.Status.CephStatus == nil || cephCluster.Status.CephStatus.FSID == "" {
		return reconcile.Result{}, fmt.Errorf("waiting for Ceph FSID")
	} else {
		fsid = cephCluster.Status.CephStatus.FSID
	}

	rbdStorageID := util.CalculateCephRbdStorageID(fsid, "")
	cephFsStorageID := util.CalculateCephFsStorageID(fsid, "csi")

	rbdClusterID, rbdProvisionerSecret, err := r.getClusterIDAndSecretName(instance, util.RbdSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}
	rbdSnapClass := SnapshotClassConfiguration{
		snapshotClass:     util.NewDefaultRbdSnapshotClass(rbdClusterID, rbdProvisionerSecret, instance.Namespace, rbdStorageID),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy),
	}
	rbdSnapClass.snapshotClass.Name = util.GenerateNameForSnapshotClass(instance.Name, util.RbdSnapshotter)
	util.AddLabel(rbdSnapClass.snapshotClass, util.ExternalClassLabelKey, strconv.FormatBool(true))

	cephfsClusterID, cephfsProvisionerSecret, err := r.getClusterIDAndSecretName(instance, util.CephfsSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}
	cephFsSnapClass := SnapshotClassConfiguration{
		snapshotClass:     util.NewDefaultCephFsSnapshotClass(cephfsClusterID, cephfsProvisionerSecret, instance.Namespace, cephFsStorageID),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
	}
	cephFsSnapClass.snapshotClass.Name = util.GenerateNameForSnapshotClass(instance.Name, util.CephfsSnapshotter)
	util.AddLabel(cephFsSnapClass.snapshotClass, util.ExternalClassLabelKey, strconv.FormatBool(true))

	volumeSnapshotClasses := []SnapshotClassConfiguration{rbdSnapClass, cephFsSnapClass}
	err = r.createSnapshotClasses(volumeSnapshotClasses)
	if err != nil {
		return reconcile.Result{}, err
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
