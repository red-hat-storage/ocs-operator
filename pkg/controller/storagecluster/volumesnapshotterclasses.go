package storagecluster

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"

	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SnapshotterType represents a snapshotter type
type SnapshotterType string

const (
	rbdSnapshotter    SnapshotterType = "rbd"
	cephfsSnapshotter SnapshotterType = "cephfs"
)

// secret name and namespace for snapshotter class
const (
	snapshotterSecretName      = "csi.storage.k8s.io/snapshotter-secret-name"
	snapshotterSecretNamespace = "csi.storage.k8s.io/snapshotter-secret-namespace"
)

// newVolumeSnapshotClass returns a new VolumeSnapshotter class backed by provided snapshotter type
// available 'snapShotterType' values are 'rbd' and 'cephfs'
func newVolumeSnapshotClass(instance *ocsv1.StorageCluster, snapShotterType SnapshotterType) *snapapi.VolumeSnapshotClass {
	retSC := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateNameForSnapshotClass(instance, snapShotterType),
		},
		Driver: generateNameForSnapshotClassDriver(instance, snapShotterType),
		Parameters: map[string]string{
			"clusterID":                instance.Namespace,
			snapshotterSecretName:      generateNameForSnapshotClassSecret(snapShotterType),
			snapshotterSecretNamespace: instance.Namespace,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	return retSC
}

func newSnapshotClasses(instance *ocsv1.StorageCluster) []*snapapi.VolumeSnapshotClass {
	scs := []*snapapi.VolumeSnapshotClass{
		newVolumeSnapshotClass(instance, rbdSnapshotter),
		newVolumeSnapshotClass(instance, cephfsSnapshotter),
	}
	return scs
}

func (r *ReconcileStorageCluster) createSnapshotClasses(vscs []*snapapi.VolumeSnapshotClass, instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	for _, vsc := range vscs {
		existing := &snapapi.VolumeSnapshotClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: vsc.Name, Namespace: vsc.Namespace}, existing)
		if err != nil {
			if errors.IsNotFound(err) {
				// Since the SnapshotClass is not found, we will create a new one
				reqLogger.Info(fmt.Sprintf("creating SnapshotClass %q", vsc.Name))
				err = r.client.Create(context.TODO(), vsc)
				if err != nil {
					reqLogger.Error(err, fmt.Sprintf("failed to create SnapshotClass %q", vsc.Name))
					return err
				}
				// no error, continue with the next iteration
				continue
			} else {
				reqLogger.Error(err, fmt.Sprintf("failed to 'Get' SnapshotClass %q", vsc.Name))
				return err
			}
		}
		reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.SnapshotClasses.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyDefault || reconcileStrategy == ReconcileStrategyUnknown {
			return nil
		}
		if existing.DeletionTimestamp != nil {
			return fmt.Errorf("failed to restore snapshotclass %q because it is marked for deletion", existing.Name)
		}
		// if there is a mis-match in the parameters of existing vs created resources,
		if !reflect.DeepEqual(vsc.Parameters, existing.Parameters) {
			// we have to update the existing SnapshotClass
			reqLogger.Info(fmt.Sprintf("SnapshotClass %q needs to be updated", existing.Name))
			existing.ObjectMeta.OwnerReferences = vsc.ObjectMeta.OwnerReferences
			vsc.ObjectMeta = existing.ObjectMeta
			if err := r.client.Update(context.TODO(), vsc); err != nil {
				reqLogger.Error(err, fmt.Sprintf("SnapshotClass %q updation failed", existing.Name))
				return err
			}
		}
	}
	return nil
}

// ensureSnapshotClasses functions ensures that snpashotter classes are created
func (r *ReconcileStorageCluster) ensureSnapshotClasses(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.SnapshotClasses.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	scs := newSnapshotClasses(instance)

	err := r.createSnapshotClasses(scs, instance, reqLogger)
	if err != nil {
		return nil
	}

	return nil
}
