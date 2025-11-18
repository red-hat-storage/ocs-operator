package storagecluster

import (
	"fmt"
	"reflect"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsOdfGroupSnapshotClass struct{}

type OdfGroupSnapshotClassConfiguration struct {
	groupSnapshotClass *odfgsapiv1b1.VolumeGroupSnapshotClass
	reconcileStrategy  ReconcileStrategy
}

func (r *StorageClusterReconciler) createOdfGroupSnapshotClasses(vgsc OdfGroupSnapshotClassConfiguration) error {
	if vgsc.reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}
	vsc := vgsc.groupSnapshotClass
	existing := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
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
			return nil
		}

		r.Log.Error(err, "Failed to 'Get' GroupSnapshotClass.", "GroupSnapshotClass", klog.KRef("", vsc.Name))
		return err
	}
	if vgsc.reconcileStrategy == ReconcileStrategyInit {
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

	return nil
}

func (obj *ocsOdfGroupSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if !r.AvailableCrds[OdfVolumeGroupSnapshotClassCrdName] {
		r.Log.Info("OdfVolumeGroupSnapshotClass CRD is not available")
		return reconcile.Result{}, nil
	}

	if true {
		vgsc := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
		vgsc.Name = util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter)
		if err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(vgsc), vgsc); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "failed to get OdfGroupSnapshotClass")
			return reconcile.Result{}, err
		} else if vgsc.UID != "" {
			r.Log.Info("Deleting OdfGroupSnapshotClass", "OdfGroupSnapshotClass", client.ObjectKeyFromObject(vgsc))
			if err := r.Client.Delete(r.ctx, vgsc); err != nil {
				r.Log.Error(err, "failed to delete OdfGroupSnapshotClass")
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	cephfsClusterID, cephfsProvisionerSecret, err := r.getClusterIDAndSecretName(instance, util.CephfsSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}

	cephFsGroupSnapshotClass := OdfGroupSnapshotClassConfiguration{
		groupSnapshotClass: &odfgsapiv1b1.VolumeGroupSnapshotClass{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{},
				Labels:      map[string]string{},
			},
			Driver: util.CephFSDriverName,
			Parameters: map[string]string{
				"clusterID": cephfsClusterID,
				"csi.storage.k8s.io/group-snapshotter-secret-name":      cephfsProvisionerSecret,
				"csi.storage.k8s.io/group-snapshotter-secret-namespace": instance.Namespace,
				"fsName": util.GenerateNameForCephFilesystem(instance.Name),
			},
			DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
		},
		reconcileStrategy: ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
	}
	cephFsGroupSnapshotClass.groupSnapshotClass.Name = util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter)
	util.AddLabel(cephFsGroupSnapshotClass.groupSnapshotClass, util.ExternalClassLabelKey, strconv.FormatBool(true))

	err = r.createOdfGroupSnapshotClasses(cephFsGroupSnapshotClass)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (obj *ocsOdfGroupSnapshotClass) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if !r.AvailableCrds[OdfVolumeGroupSnapshotClassCrdName] {
		r.Log.Info("OdfVolumeGroupSnapshotClass CRD doesn't exist")
		return reconcile.Result{}, nil
	}

	vgsc := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
	vgsc.Name = util.GenerateNameForGroupSnapshotClass(instance, util.CephfsGroupSnapshotter)
	vgsc.Namespace = instance.Namespace
	err := r.Client.Delete(r.ctx, vgsc)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: OdfGroupSnapshotClass not found, nothing to do.", "OdfGroupSnapshotClass", klog.KRef("", vgsc.Name))
		} else {
			r.Log.Error(err, "Uninstall: Error while deleting OdfGroupSnapshotClass.", "OdfGroupSnapshotClass", klog.KRef("", vgsc.Name))
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}
