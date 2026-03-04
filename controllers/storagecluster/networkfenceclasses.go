package storagecluster

import (
	"encoding/json"
	"fmt"
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

func (obj *ocsNetworkFenceClass) deleteNetworkFenceClass(r *StorageClusterReconciler, networkFenceClass *csiaddonsv1alpha1.NetworkFenceClass) (reconcile.Result, error) {

	if networkFenceClass.DeletionTimestamp != nil {
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for NetworkFenceClass %v to be deleted", client.ObjectKeyFromObject(networkFenceClass))
	}

	r.Log.Info("Uninstall: Deleting NetworkFenceClass.", "NetworkFenceClass", client.ObjectKeyFromObject(networkFenceClass))
	if err := r.Client.Delete(r.ctx, networkFenceClass); err != nil {
		return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete NetworkFenceClass %v: %v", client.ObjectKeyFromObject(networkFenceClass), err)
	}
	// Requeue as we need to wait till cephBlockPool is deleted
	return reconcile.Result{Requeue: true}, nil
}

func (obj *ocsNetworkFenceClass) reconcileRbdNetworkFenceClass(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(storageCluster.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	rbdNetworkFenceClass := &csiaddonsv1alpha1.NetworkFenceClass{}
	rbdNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)

	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(rbdNetworkFenceClass), rbdNetworkFenceClass)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	// storageCluster is marked for deletion - delete it
	if storageCluster.GetDeletionTimestamp() != nil {
		// if found, delete the block pool
		if !errors.IsNotFound(err) {
			return obj.deleteNetworkFenceClass(r, rbdNetworkFenceClass)
		}
		return reconcile.Result{}, nil
	}

	// If found and reconcileStrategy is init we skip
	if !errors.IsNotFound(err) && reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, rbdNetworkFenceClass, func() error {
		rbdClusterID, rbdProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.RbdSnapshotter)
		if err != nil {
			return err
		}
		nfc := util.NewDefaultRbdNetworkFenceClass(rbdClusterID, rbdProvisionerSecret, storageCluster.Namespace, "")

		// Unmarshal follows merge semantics, that means that we don't need to worry about overriding the status,
		// or any metadata fields. There is an exception when it comes to creationTimestamp which gets serialized into
		// default value.
		creationTimestamp := rbdNetworkFenceClass.CreationTimestamp
		nfcBytes := util.JsonMustMarshal(nfc)
		if err := json.Unmarshal(nfcBytes, rbdNetworkFenceClass); err != nil {
			return err
		}
		rbdNetworkFenceClass.SetCreationTimestamp(creationTimestamp)

		util.AddLabel(rbdNetworkFenceClass, util.ExternalClassLabelKey, strconv.FormatBool(true))
		return nil
	})
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (obj *ocsNetworkFenceClass) reconcileCephFsNetworkFenceClass(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(storageCluster.Spec.ManagedResources.CephFilesystems.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	cephFsNetworkFenceClass := &csiaddonsv1alpha1.NetworkFenceClass{}
	cephFsNetworkFenceClass.Name = util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)

	err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(cephFsNetworkFenceClass), cephFsNetworkFenceClass)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	// storageCluster is marked for deletion - delete it
	if storageCluster.GetDeletionTimestamp() != nil {
		// if found, delete the block pool
		if !errors.IsNotFound(err) {
			return obj.deleteNetworkFenceClass(r, cephFsNetworkFenceClass)
		}
		return reconcile.Result{}, nil
	}

	// If found and reconcileStrategy is init we skip
	if !errors.IsNotFound(err) && reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, cephFsNetworkFenceClass, func() error {
		cephFsClusterID, cephFsProvisionerSecret, err := r.getClusterIDAndSecretName(storageCluster, util.CephfsSnapshotter)
		if err != nil {
			return err
		}
		nfc := util.NewDefaultCephfsNetworkFenceClass(cephFsClusterID, cephFsProvisionerSecret, storageCluster.Namespace, "")

		// Unmarshal follows merge semantics, that means that we don't need to worry about overriding the status,
		// or any metadata fields. There is an exception when it comes to creationTimestamp which gets serialized into
		// default value.
		creationTimestamp := cephFsNetworkFenceClass.CreationTimestamp
		nfcBytes := util.JsonMustMarshal(nfc)
		if err := json.Unmarshal(nfcBytes, cephFsNetworkFenceClass); err != nil {
			return err
		}
		cephFsNetworkFenceClass.SetCreationTimestamp(creationTimestamp)

		util.AddLabel(cephFsNetworkFenceClass, util.ExternalClassLabelKey, strconv.FormatBool(true))
		return nil
	})
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

// ensureCreated functions ensures that NetworkFenceClass classes are created
func (obj *ocsNetworkFenceClass) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if res, err := obj.reconcileRbdNetworkFenceClass(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := obj.reconcileCephFsNetworkFenceClass(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the NetworkFenceClass that the ocs-operator created
func (obj *ocsNetworkFenceClass) ensureDeleted(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if res, err := obj.reconcileRbdNetworkFenceClass(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	if res, err := obj.reconcileCephFsNetworkFenceClass(r, storageCluster); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}
