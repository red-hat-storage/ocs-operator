package storagecluster

import (
	"context"
	"fmt"
	"reflect"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SnapshotterType represents a snapshotter type
type SnapshotterType string

type ocsSnapshotClass struct{}

const (
	RbdSnapshotter    SnapshotterType = "rbd"
	CephfsSnapshotter SnapshotterType = "cephfs"
	NfsSnapshotter    SnapshotterType = "nfs"
)

// secret name and namespace for snapshotter class
const (
	snapshotterSecretName      = "csi.storage.k8s.io/snapshotter-secret-name"
	snapshotterSecretNamespace = "csi.storage.k8s.io/snapshotter-secret-namespace"
)

// SnapshotClassConfiguration provides configuration options for a SnapshotClass.
type SnapshotClassConfiguration struct {
	snapshotClass     *snapapi.VolumeSnapshotClass
	reconcileStrategy ReconcileStrategy
	disable           bool
}

// newVolumeSnapshotClass returns a new VolumeSnapshotter class backed by provided snapshotter type
// available 'snapShotterType' values are 'rbd','cephfs' and 'cephnfs'
func newVolumeSnapshotClass(instance *ocsv1.StorageCluster, snapShotterType SnapshotterType) *snapapi.VolumeSnapshotClass {
	retSC := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: GenerateNameForSnapshotClass(instance, snapShotterType),
		},
		Driver: generateNameForSnapshotClassDriver(snapShotterType),
		Parameters: map[string]string{
			"clusterID":                instance.Namespace,
			snapshotterSecretName:      generateNameForSnapshotClassSecret(instance, snapShotterType),
			snapshotterSecretNamespace: instance.Namespace,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	return retSC
}

func newCephFilesystemSnapshotClassConfiguration(instance *ocsv1.StorageCluster) SnapshotClassConfiguration {
	return SnapshotClassConfiguration{
		snapshotClass:     newVolumeSnapshotClass(instance, CephfsSnapshotter),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
		disable:           instance.Spec.ManagedResources.CephFilesystems.DisableSnapshotClass || instance.Spec.AllowRemoteStorageConsumers,
	}
}

func newCephBlockPoolSnapshotClassConfiguration(instance *ocsv1.StorageCluster) SnapshotClassConfiguration {
	return SnapshotClassConfiguration{
		snapshotClass:     newVolumeSnapshotClass(instance, RbdSnapshotter),
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy),
		disable:           instance.Spec.ManagedResources.CephBlockPools.DisableSnapshotClass || instance.Spec.AllowRemoteStorageConsumers,
	}
}

func newCephNetworkFilesystemSnapshotClassConfiguration(instance *ocsv1.StorageCluster) SnapshotClassConfiguration {
	return SnapshotClassConfiguration{
		snapshotClass: newVolumeSnapshotClass(instance, NfsSnapshotter),
	}
}

// newSnapshotClassConfigurations generates configuration options for Ceph SnapshotClasses.
func newSnapshotClassConfigurations(instance *ocsv1.StorageCluster) []SnapshotClassConfiguration {
	vsccs := []SnapshotClassConfiguration{
		newCephFilesystemSnapshotClassConfiguration(instance),
		newCephBlockPoolSnapshotClassConfiguration(instance),
	}
	if instance.Spec.NFS != nil && instance.Spec.NFS.Enable {
		vsccs = append(vsccs, newCephNetworkFilesystemSnapshotClassConfiguration(instance))
	}
	return vsccs
}

func (r *StorageClusterReconciler) createSnapshotClasses(vsccs []SnapshotClassConfiguration) error {

	for _, vscc := range vsccs {
		if vscc.reconcileStrategy == ReconcileStrategyIgnore || vscc.disable {
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

	vsccs := newSnapshotClassConfigurations(instance)

	err := r.createSnapshotClasses(vsccs)
	if err != nil {
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the SnapshotClasses that the ocs-operator created
func (obj *ocsSnapshotClass) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	vsccs := newSnapshotClassConfigurations(instance)
	for _, vscc := range vsccs {
		sc := vscc.snapshotClass
		existing := snapapi.VolumeSnapshotClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				r.Log.Info("Uninstall: SnapshotClass is already marked for deletion.", "SnapshotClass", klog.KRef(existing.Namespace, existing.Name))
				break
			}

			r.Log.Info("Uninstall: Deleting SnapshotClass.", "SnapshotClass", klog.KRef(existing.Namespace, existing.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.Client.Delete(context.TODO(), sc)
			if err != nil {
				r.Log.Error(err, "Uninstall: Ignoring error deleting the SnapshotClass.", "SnapshotClass", klog.KRef(existing.Namespace, existing.Name))
			}
		case errors.IsNotFound(err):
			r.Log.Info("Uninstall: SnapshotClass not found, nothing to do.", "SnapshotClass", klog.KRef(sc.Namespace, sc.Name))
		default:
			r.Log.Error(err, "Uninstall: Error while getting SnapshotClass.", "SnapshotClass", klog.KRef(sc.Namespace, sc.Name))
		}
	}
	return reconcile.Result{}, nil
}
