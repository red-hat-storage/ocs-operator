package storagecluster

import (
	"encoding/json"
	"fmt"
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

	vsc := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
	vsc.Name = vgsc.groupSnapshotClass.Name
	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(vsc), vsc)
	if client.IgnoreNotFound(err) != nil {
		r.Log.Error(err, "Failed to 'Get' GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(vsc))
		return err
	}

	// If found and reconcileStrategy is init we skip
	if !errors.IsNotFound(err) && vgsc.reconcileStrategy == ReconcileStrategyInit {
		return nil
	}

	if vsc.DeletionTimestamp != nil {
		return fmt.Errorf("failed to restore GroupSnapshotClass %q because it is marked for deletion", vsc.Name)
	}

	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, vsc, func() error {

		// Unmarshal follows merge semantics, that means that we don't need to worry about overriding the status,
		// or any metadata fields. There is an exception when it comes to creationTimestamp which gets serialized into
		// default value.
		desiredBytes := util.JsonMustMarshal(vgsc.groupSnapshotClass)
		creationTimestamp := vsc.GetCreationTimestamp()
		if err := json.Unmarshal(desiredBytes, vsc); err != nil {
			return fmt.Errorf("failed to unmarshal %s configuration response: %v", vsc.GetName(), err)
		}
		vsc.SetCreationTimestamp(creationTimestamp)
		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create or update GroupSnapshotClass.", "GroupSnapshotClass", client.ObjectKeyFromObject(vsc))
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
