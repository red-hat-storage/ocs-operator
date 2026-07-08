package storagecluster

import (
	"fmt"
	"maps"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsNetworkFenceClass struct{}

func (o *ocsNetworkFenceClass) reconcileNetworkFenceClass(
	r *StorageClusterReconciler,
	networkFenceClass *csiaddonsv1alpha1.NetworkFenceClass,
	reconcileStrategy ReconcileStrategy,
) (reconcile.Result, error) {
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	existing := &csiaddonsv1alpha1.NetworkFenceClass{}
	existing.Name = networkFenceClass.Name

	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, existing, func() error {

		if existing.UID != "" && reconcileStrategy == ReconcileStrategyInit {
			return nil
		}

		if len(existing.Labels) == 0 {
			existing.Labels = map[string]string{}
		}
		if len(existing.Annotations) == 0 {
			existing.Annotations = map[string]string{}
		}
		maps.Copy(existing.Labels, networkFenceClass.Labels)
		maps.Copy(existing.Annotations, networkFenceClass.Annotations)
		util.AddLabel(existing, util.ExternalClassLabelKey, strconv.FormatBool(true))

		existing.Spec = networkFenceClass.Spec

		return nil
	}); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil

}

func (o *ocsNetworkFenceClass) reconcileRbdNetworkFenceClass(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster, cephFsid string) (reconcile.Result, error) {

	rbdClusterID, rbdProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.RbdSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}

	rbdStorageID := util.CalculateCephRbdStorageID(cephFsid, getExternalModeRadosNamespaceName(storageCluster))

	rbdNetworkFenceClass := util.NewDefaultRbdNetworkFenceClass(rbdClusterID, rbdProvisionerSecret, storageCluster.Namespace, rbdStorageID)
	rbdNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)

	return o.reconcileNetworkFenceClass(
		r,
		rbdNetworkFenceClass,
		ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy),
	)
}

func (o *ocsNetworkFenceClass) reconcileCephFsNetworkFenceClass(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster, cephFsid string) (reconcile.Result, error) {

	cephFsClusterID, cephFsProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.CephfsSnapshotter)
	if err != nil {
		return reconcile.Result{}, err
	}

	cephFsStorageID := util.CalculateCephFsStorageID(cephFsid, "csi")

	cephFsNetworkFenceClass := util.NewDefaultCephfsNetworkFenceClass(cephFsClusterID, cephFsProvisionerSecret, storageCluster.Namespace, cephFsStorageID)
	cephFsNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.CephfsNetworkFenceClass)

	return o.reconcileNetworkFenceClass(
		r,
		cephFsNetworkFenceClass,
		ReconcileStrategy(storageCluster.Spec.ManagedResources.CephFilesystems.ReconcileStrategy),
	)
}

// ensureCreated functions ensures that NetworkFenceClass classes are created
func (o *ocsNetworkFenceClass) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	var fsid string
	if cephCluster, err := util.GetCephClusterInNamespace(r.ctx, r.Client, storageCluster.Namespace); err != nil {
		return reconcile.Result{}, err
	} else if cephCluster.Status.CephStatus == nil || cephCluster.Status.CephStatus.FSID == "" {
		return reconcile.Result{}, fmt.Errorf("waiting for Ceph FSID")
	} else {
		fsid = cephCluster.Status.CephStatus.FSID
	}

	if res, err := o.reconcileRbdNetworkFenceClass(r, storageCluster, fsid); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := o.reconcileCephFsNetworkFenceClass(r, storageCluster, fsid); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the NetworkFenceClass that the ocs-operator created
func (o *ocsNetworkFenceClass) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	names := []string{
		util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass),
		util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.CephfsNetworkFenceClass),
	}
	for _, name := range names {
		nfc := &csiaddonsv1alpha1.NetworkFenceClass{}
		nfc.Name = name
		if err := r.Get(r.ctx, client.ObjectKeyFromObject(nfc), nfc); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "failed to get NetworkFenceClass", "NetworkFenceClass", client.ObjectKeyFromObject(nfc))
			return reconcile.Result{}, err
		} else if nfc.UID != "" {
			if err := r.Delete(r.ctx, nfc); client.IgnoreNotFound(err) != nil {
				r.Log.Error(err, "failed to delete NetworkFenceClass.", "NetworkFenceClass", client.ObjectKeyFromObject(nfc))
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}
