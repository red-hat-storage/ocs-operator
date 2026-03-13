package storagecluster

import (
	"fmt"
	"golang.org/x/exp/maps"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	desired := vgsc.groupSnapshotClass
	existing := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
	existing.Name = desired.Name
	if err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(existing), existing); client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to 'Get' GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(existing))
		return err
	}

	// If found and reconcileStrategy is init we skip
	if existing.UID != "" && vgsc.reconcileStrategy == ReconcileStrategyInit {
		return nil
	}

	if !existing.DeletionTimestamp.IsZero() {
		return fmt.Errorf("failed to restore GroupSnapshotClass %q because it is marked for deletion", existing.Name)
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
		r.Log.Error(err, "Failed to create or update GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(existing))
		return err
	}

	return nil
}

func (obj *ocsOdfGroupSnapshotClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if val, _ := r.crdsBeingWatched.Load(OdfVolumeGroupSnapshotClassCrdName); !val.(bool) {
		r.Log.Info("OdfVolumeGroupSnapshotClass CRD is not available")
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
	if val, _ := r.crdsBeingWatched.Load(OdfVolumeGroupSnapshotClassCrdName); !val.(bool) {
		r.Log.Info("OdfVolumeGroupSnapshotClass CRD is not available")
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
