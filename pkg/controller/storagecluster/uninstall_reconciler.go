package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// CleanupPolicyType is a string representing cleanup policy
type CleanupPolicyType string

// UninstallModeType is a string representing cleanup mode, it decides whether the deletion is graceful or forced
type UninstallModeType string

const (
	// CleanupPolicyAnnotation defines the cleanup policy for data and metadata during uninstall
	CleanupPolicyAnnotation = "uninstall.ocs.openshift.io/cleanup-policy"
	// CleanupPolicyDelete when set, modifies the cleanup policy for Rook to delete the DataDirHostPath on uninstall
	CleanupPolicyDelete CleanupPolicyType = "delete"
	// CleanupPolicyRetain when set, modifies the cleanup policy for Rook to not cleanup the DataDirHostPath and the disks on uninstall
	CleanupPolicyRetain CleanupPolicyType = "retain"
	// UninstallModeAnnotation defines the uninstall mode
	UninstallModeAnnotation = "uninstall.ocs.openshift.io/mode"
	// UninstallModeForced when set, sets the uninstall mode for Rook and Noobaa to forced.
	UninstallModeForced UninstallModeType = "forced"
	// UninstallModeGraceful when set, sets the uninstall mode for Rook and Noobaa to graceful.
	UninstallModeGraceful UninstallModeType = "graceful"
)

// deleteStorageClasses deletes the storageClasses that the ocs-operator created
func (r *ReconcileStorageCluster) deleteStorageClasses(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	scs, err := r.newStorageClasses(instance)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to determine the StorageClass names"))
		return nil
	}
	for _, sc := range scs {
		existing := storagev1.StorageClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Uninstall: StorageClass %s is already marked for deletion", existing.Name))
				break
			}

			reqLogger.Info(fmt.Sprintf("Uninstall: Deleting StorageClass %s", sc.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.client.Delete(context.TODO(), sc)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Ignoring error deleting the StorageClass %s", existing.Name))
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Uninstall: StorageClass %s not found, nothing to do", sc.Name))
		default:
			reqLogger.Info(fmt.Sprintf("Uninstall: Error while getting StorageClass %s: %v", sc.Name, err))
		}
	}
	return nil
}

// deleteNodeAffinityKeyFromNodes deletes the default NodeAffinityKey from the OCS nodes
func (r *ReconcileStorageCluster) deleteNodeAffinityKeyFromNodes(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (err error) {

	// We should delete the label only when the StorageCluster is using the default NodeAffinityKey
	if sc.Spec.LabelSelector == nil {
		nodes, err := r.getStorageClusterEligibleNodes(sc, reqLogger)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to obtain the list of nodes eligible for the Storage Cluster"))
			return nil
		}
		for _, node := range nodes.Items {
			reqLogger.Info(fmt.Sprintf("Uninstall: Deleting OCS label from node %s", node.Name))
			new := node.DeepCopy()
			delete(new.ObjectMeta.Labels, defaults.NodeAffinityKey)

			oldJSON, err := json.Marshal(node)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			newJSON, err := json.Marshal(new)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			err = r.client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

		}

	}
	return nil
}

// deleteNodeTaint deletes the default NodeTolerationKey from the OCS nodes
func (r *ReconcileStorageCluster) deleteNodeTaint(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (err error) {

	nodes, err := r.getStorageClusterEligibleNodes(sc, reqLogger)
	if err != nil {
		reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to obtain the list of nodes eligible for the Storage Cluster"))
		return nil
	}
	for _, node := range nodes.Items {
		reqLogger.Info(fmt.Sprintf("Uninstall: Deleting OCS NodeTolerationKey from the node %s", node.Name))
		new := node.DeepCopy()
		new.Spec.Taints = make([]corev1.Taint, 0)
		for _, taint := range node.Spec.Taints {
			if defaults.NodeTolerationKey == taint.Key {
				continue
			}
			new.Spec.Taints = append(new.Spec.Taints, taint)
		}

		oldJSON, err := json.Marshal(node)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		newJSON, err := json.Marshal(new)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		err = r.client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Uninstall: Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

	}

	return nil
}

// deleteCephCluster deletes the CephCluster owned by the StorageCluster
func (r *ReconcileStorageCluster) deleteCephCluster(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	cephCluster := &cephv1.CephCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Uninstall: CephCluster not found")
			return nil
		}
		return fmt.Errorf("Uninstall: Unable to retrive cephCluster: %v", err)
	}

	if cephCluster.GetDeletionTimestamp().IsZero() {
		reqLogger.Info("Uninstall: Deleting cephCluster")
		err = r.client.Delete(context.TODO(), cephCluster)
		if err != nil {
			return fmt.Errorf("Uninstall: Failed to delete cephCluster: %v", err)
		}
	}

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Uninstall: CephCluster is deleted")
			return nil
		}
	}
	return fmt.Errorf("Uninstall: Waiting for cephCluster to be deleted")

}

// setRookUninstallandCleanupPolicy sets the uninstall mode and cleanup policy for rook based on the annotation on the StorageCluster
func (r *ReconcileStorageCluster) setRookUninstallandCleanupPolicy(instance *ocsv1.StorageCluster, reqLogger logr.Logger) (err error) {

	cephCluster := &cephv1.CephCluster{}
	var updateRequired bool

	err = r.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(instance), Namespace: instance.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Uninstall: CephCluster not found, can't set the cleanup policy and uninstall mode")
			return nil
		}
		return fmt.Errorf("Uninstall: Unable to retrieve the cephCluster: %v", err)
	}

	if v, found := instance.ObjectMeta.Annotations[CleanupPolicyAnnotation]; found {
		if (v == string(CleanupPolicyDelete)) && (cephCluster.Spec.CleanupPolicy.Confirmation != cephv1.DeleteDataDirOnHostsConfirmation) {
			cephCluster.Spec.CleanupPolicy.Confirmation = cephv1.DeleteDataDirOnHostsConfirmation
			updateRequired = true
		} else if (v == string(CleanupPolicyRetain)) && (cephCluster.Spec.CleanupPolicy.Confirmation != "") {
			cephCluster.Spec.CleanupPolicy.Confirmation = ""
			updateRequired = true
		}
	}

	if v, found := instance.ObjectMeta.Annotations[UninstallModeAnnotation]; found {
		if (v == string(UninstallModeForced)) && (cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes != true) {
			cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes = true
			updateRequired = true
		} else if (v == string(UninstallModeGraceful)) && (cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes != false) {
			cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes = false
			updateRequired = true
		}
	}

	if updateRequired {
		err := r.client.Update(context.TODO(), cephCluster)
		if err != nil {
			return fmt.Errorf("Uninstall: Unable to update the cephCluster to set uninstall mode and/or cleanup policy: %v", err)
		}
		reqLogger.Info("Uninstall: CephCluster uninstall mode and cleanup policy has been set")
	}

	return nil
}

// setNoobaaUninstallMode sets the uninstall mode for Noobaa based on the annotation on the StorageCluster
func (r *ReconcileStorageCluster) setNoobaaUninstallMode(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	noobaa := &nbv1.NooBaa{}
	var updateRequired bool

	err := r.client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Uninstall: NooBaa not found, can't set UninstallModeForced")
			return nil
		}
		return fmt.Errorf("Uninstall: Error while getting NooBaa %v", err)
	}

	// The CleanupPolicy attribute in the Noobaa spec decides the uninstall mode.
	// Unlike the Rook CleanupPolicy which decides whether the data needs to be erased.
	if v, found := sc.ObjectMeta.Annotations[UninstallModeAnnotation]; found {
		if (v == string(UninstallModeForced)) && (noobaa.Spec.CleanupPolicy.Confirmation != nbv1.DeleteOBCConfirmation) {
			noobaa.Spec.CleanupPolicy.Confirmation = nbv1.DeleteOBCConfirmation
			updateRequired = true
		} else if (v == string(UninstallModeGraceful)) && (noobaa.Spec.CleanupPolicy.Confirmation != "") {
			noobaa.Spec.CleanupPolicy.Confirmation = ""
			updateRequired = true
		}
	}

	if updateRequired {
		err = r.client.Update(context.TODO(), noobaa)
		if err != nil {
			return fmt.Errorf("Uninstall: Unable to update NooBaa uninstall mode: %v", err)
		}
		reqLogger.Info("Uninstall: NooBaa uninstall mode has been set")
	}

	return nil
}

// reconcileUninstallAnnotations looks at the current uninstall annotations on the StorageCluster and sets defaults if none or unrecognized ones are set.
func (r *ReconcileStorageCluster) reconcileUninstallAnnotations(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	var updateRequired bool

	if v, found := sc.ObjectMeta.Annotations[UninstallModeAnnotation]; !found {
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(UninstallModeAnnotation), string(UninstallModeGraceful))
		reqLogger.Info("Uninstall: Setting uninstall mode annotation to default", "UninstallMode", UninstallModeGraceful)
		updateRequired = true
	} else if found && v != string(UninstallModeGraceful) && v != string(UninstallModeForced) {
		// if wrong value found
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(UninstallModeAnnotation), string(UninstallModeGraceful))
		reqLogger.Info("Uninstall: Found unrecognized uninstall mode annotation. Changing it to default",
			"CurrentUninstallMode", v, "DefaultUninstallMode", UninstallModeGraceful)
		updateRequired = true
	}

	if v, found := sc.ObjectMeta.Annotations[CleanupPolicyAnnotation]; !found {
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(CleanupPolicyAnnotation), string(CleanupPolicyDelete))
		reqLogger.Info("Uninstall: Setting uninstall cleanup policy annotation to default", "CleanupPolicy", CleanupPolicyDelete)
		updateRequired = true
	} else if found && v != string(CleanupPolicyDelete) && v != string(CleanupPolicyRetain) {
		// if wrong value found
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(CleanupPolicyAnnotation), string(CleanupPolicyDelete))
		reqLogger.Info("Uninstall: Found unrecognized uninstall cleanup policy annotation.Changing it to default",
			"CurrentCleanupPolicy", v, "DefaultCleanupPolicy", CleanupPolicyDelete)
		updateRequired = true
	}

	if updateRequired {
		if err := r.client.Update(context.TODO(), sc); err != nil {
			reqLogger.Error(err, "Uninstall: Failed to update the storagecluster with uninstall defaults")
			return err
		}
		reqLogger.Info("Uninstall: Default uninstall annotations has been set on storagecluster")
	}
	return nil
}

// deleteResources is the function where the storageClusterFinalizer is handled
// Every function that is called within this function should be idempotent
func (r *ReconcileStorageCluster) deleteResources(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	err := r.setRookUninstallandCleanupPolicy(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.setNoobaaUninstallMode(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteNoobaaSystems(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteCephCluster(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteCephObjectStoreUsers(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteCephObjectStores(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteCephFilesystems(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteCephBlockPools(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteSnapshotClasses(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteStorageClasses(sc, reqLogger)
	if err != nil {
		return err
	}

	err = r.deleteNodeTaint(sc, reqLogger)
	if err != nil {
		return err
	}

	// TODO: skip the deletion of these labels till we figure out a way to wait
	// for the cleanup jobs
	//err = r.deleteNodeAffinityKeyFromNodes(sc, reqLogger)
	//if err != nil {
	//	return err
	//}

	return nil
}

// deleteCephFilesystems deletes the CephFilesystems owned by the StorageCluster
func (r *ReconcileStorageCluster) deleteCephFilesystems(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	foundCephFilesystem := &cephv1.CephFilesystem{}
	cephFilesystems, err := r.newCephFilesystemInstances(sc)
	if err != nil {
		return err
	}

	for _, cephFilesystem := range cephFilesystems {
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephFilesystem not found", "CephFilesystem Name", cephFilesystem.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrive cephFilesystem %v: %v", cephFilesystem.Name, err)
		}

		if cephFilesystem.GetDeletionTimestamp().IsZero() {
			reqLogger.Info("Uninstall: Deleting cephFilesystem", "CephFilesystem Name", cephFilesystem.Name)
			err = r.client.Delete(context.TODO(), foundCephFilesystem)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephFilesystem %v: %v", foundCephFilesystem.Name, err)
			}
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephFilesystem is deleted", "CephFilesystem Name", cephFilesystem.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephFilesystem %v to be deleted", cephFilesystem.Name)

	}
	return nil
}

// deleteCephBlockPools deletes the CephBlockPools owned by the StorageCluster
func (r *ReconcileStorageCluster) deleteCephBlockPools(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	foundCephBlockPool := &cephv1.CephBlockPool{}
	cephBlockPools, err := r.newCephBlockPoolInstances(sc)
	if err != nil {
		return err
	}

	for _, cephBlockPool := range cephBlockPools {
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephBlockPool not found", "CephBlockPool Name", cephBlockPool.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrive cephBlockPool %v: %v", cephBlockPool.Name, err)
		}

		if cephBlockPool.GetDeletionTimestamp().IsZero() {
			reqLogger.Info("Uninstall: Deleting cephBlockPool", "CephBlockPool Name", cephBlockPool.Name)
			err = r.client.Delete(context.TODO(), foundCephBlockPool)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephBlockPool %v: %v", foundCephBlockPool.Name, err)
			}
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephBlockPool is deleted", "CephBlockPool Name", cephBlockPool.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephBlockPool %v to be deleted", cephBlockPool.Name)

	}
	return nil
}

// deleteCephObjectStoreUsers deletes the CephObjectStoreUsers owned by the StorageCluster
func (r *ReconcileStorageCluster) deleteCephObjectStoreUsers(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	foundCephObjectStoreUser := &cephv1.CephObjectStoreUser{}
	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(sc)
	if err != nil {
		return err
	}

	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephObjectStoreUser not found", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrive cephObjectStoreUser %v: %v", cephObjectStoreUser.Name, err)
		}

		if cephObjectStoreUser.GetDeletionTimestamp().IsZero() {
			reqLogger.Info("Uninstall: Deleting cephObjectStoreUser", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
			err = r.client.Delete(context.TODO(), foundCephObjectStoreUser)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephObjectStoreUser %v: %v", foundCephObjectStoreUser.Name, err)
			}
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: sc.Namespace}, foundCephObjectStoreUser)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephObjectStoreUser is deleted", "CephObjectStoreUser Name", cephObjectStoreUser.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephObjectStoreUser %v to be deleted", cephObjectStoreUser.Name)

	}
	return nil
}

// deleteCephObjectStores deletes the CephObjectStores owned by the StorageCluster
func (r *ReconcileStorageCluster) deleteCephObjectStores(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	foundCephObjectStore := &cephv1.CephObjectStore{}
	cephObjectStores, err := r.newCephObjectStoreInstances(sc, reqLogger)
	if err != nil {
		return err
	}

	for _, cephObjectStore := range cephObjectStores {
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephObjectStore not found", "CephObjectStore Name", cephObjectStore.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrive cephObjectStore %v: %v", cephObjectStore.Name, err)
		}

		if cephObjectStore.GetDeletionTimestamp().IsZero() {
			reqLogger.Info("Uninstall: Deleting cephObjectStore", "CephObjectStore Name", cephObjectStore.Name)
			err = r.client.Delete(context.TODO(), foundCephObjectStore)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete cephObjectStore %v: %v", foundCephObjectStore.Name, err)
			}
		}

		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: sc.Namespace}, foundCephObjectStore)
		if err != nil {
			if errors.IsNotFound(err) {
				reqLogger.Info("Uninstall: CephObjectStore is deleted", "CephObjectStore Name", cephObjectStore.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for cephObjectStore %v to be deleted", cephObjectStore.Name)

	}
	return nil
}

// deleteSnapshotClasses deletes the storageClasses that the ocs-operator created
func (r *ReconcileStorageCluster) deleteSnapshotClasses(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	scs := newSnapshotClasses(instance)
	for _, sc := range scs {
		existing := snapapi.VolumeSnapshotClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Uninstall: SnapshotClass %s is already marked for deletion", existing.Name))
				break
			}

			reqLogger.Info(fmt.Sprintf("Uninstall: Deleting SnapshotClass %s", sc.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.client.Delete(context.TODO(), sc)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Uninstall: Ignoring error deleting the SnapshotClass %s", existing.Name))
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Uninstall: SnapshotClass %s not found, nothing to do", sc.Name))
		default:
			reqLogger.Info(fmt.Sprintf("Uninstall: Error while getting SnapshotClass %s: %v", sc.Name, err))
		}
	}
	return nil
}
