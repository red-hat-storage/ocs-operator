package storagecluster

import (
	"context"
	"fmt"
	"golang.org/x/exp/maps"
	"strconv"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

		desired := vscc.snapshotClass
		existing := &snapapi.VolumeSnapshotClass{}
		existing.Name = desired.Name
		if err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(existing), existing); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Failed to 'Get' SnapshotClass.", "SnapshotClass", client.ObjectKeyFromObject(existing))
			return err
		}

		// If found and reconcileStrategy is init we skip
		if existing.UID != "" && vscc.reconcileStrategy == ReconcileStrategyInit {
			continue
		}

		if !existing.DeletionTimestamp.IsZero() {
			return fmt.Errorf("failed to restore SnapshotClass %q because it is marked for deletion", existing.Name)
		}

		_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, existing, func() error {
			if len(existing.Labels) == 0 {
				existing.Labels = map[string]string{}
			}
			if len(existing.Annotations) == 0 {
				existing.Annotations = map[string]string{}
			}

			maps.Copy(existing.Labels, desired.Labels)
			maps.Copy(existing.Annotations, desired.Annotations)

			existing.DeletionPolicy = desired.DeletionPolicy
			existing.Driver = desired.Driver
			existing.Parameters = desired.Parameters
			return nil
		})
		if err != nil {
			r.Log.Error(err, "Failed to create or update SnapshotClass.", "SnapshotClass", client.ObjectKeyFromObject(existing))
			return err
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

	rbdStorageID := util.CalculateCephRbdStorageID(fsid, getExternalModeRadosNamespaceName(instance))
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
