package storagecluster

import (
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsNetworkFenceClass struct{}

// ensureCreated functions ensures that NetworkFenceClass classes are created
func (obj *ocsNetworkFenceClass) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy) != ReconcileStrategyIgnore {
		rbdNetworkFenceClass := &csiaddonsv1alpha1.NetworkFenceClass{}
		rbdNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)
		_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, rbdNetworkFenceClass, func() error {
			rbdClusterID, rbdProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.RbdSnapshotter)
			if err != nil {
				return err
			}
			nfc := util.NewDefaultRbdNetworkFenceClass(rbdClusterID, rbdProvisionerSecret, storageCluster.Namespace, "")
			rbdNetworkFenceClass.Spec = nfc.Spec

			util.AddLabel(rbdNetworkFenceClass, util.ExternalClassLabelKey, strconv.FormatBool(true))
			return nil
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if ReconcileStrategy(storageCluster.Spec.ManagedResources.CephFilesystems.ReconcileStrategy) != ReconcileStrategyIgnore {
		cephFsNetworkFenceClass := &csiaddonsv1alpha1.NetworkFenceClass{}
		cephFsNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.CephfsNetworkFenceClass)
		_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, cephFsNetworkFenceClass, func() error {
			cephFsClusterID, cephFsProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.CephfsSnapshotter)
			if err != nil {
				return err
			}
			nfc := util.NewDefaultCephfsNetworkFenceClass(cephFsClusterID, cephFsProvisionerSecret, storageCluster.Namespace, "")
			cephFsNetworkFenceClass.Spec = nfc.Spec

			util.AddLabel(cephFsNetworkFenceClass, util.ExternalClassLabelKey, strconv.FormatBool(true))
			return nil
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the NetworkFenceClass that the ocs-operator created
func (obj *ocsNetworkFenceClass) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	names := []string{
		util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass),
		util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.CephfsNetworkFenceClass),
	}
	for _, name := range names {
		nfc := &csiaddonsv1alpha1.NetworkFenceClass{}
		nfc.Name = name
		if err := r.Client.Delete(r.ctx, nfc); err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: NetworkFenceClass not found, nothing to do.", "NetworkFenceClass", client.ObjectKeyFromObject(nfc))
			} else {
				r.Log.Error(err, "Uninstall: Error while deleting NetworkFenceClass.", "NetworkFenceClass", client.ObjectKeyFromObject(nfc))
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}
