package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
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

// deleteNodeAffinityKeyFromNodes deletes the default NodeAffinityKey from the OCS nodes
// This is not used, yet.
//
// nolint:unused
func (r *StorageClusterReconciler) deleteNodeAffinityKeyFromNodes(sc *ocsv1.StorageCluster) (err error) {

	// We should delete the label only when the StorageCluster is using the default NodeAffinityKey
	if sc.Spec.LabelSelector == nil {
		nodes, err := r.getStorageClusterEligibleNodes(sc)
		if err != nil {
			r.Log.Error(err, "Uninstall: Unable to obtain the list of nodes eligible for the Storage Cluster.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name)) //nolint:gosimple
			return nil
		}
		for _, node := range nodes.Items {
			r.Log.Info("Uninstall: Deleting OCS label from Node.", "Node", node.Name)
			updatedNode := node.DeepCopy()
			delete(updatedNode.ObjectMeta.Labels, defaults.NodeAffinityKey)

			oldJSON, err := json.Marshal(node)
			if err != nil {
				r.Log.Error(err, "Uninstall: Unable to remove the NodeAffinityKey from the Node.", "Node", node.Name)
				continue
			}

			newJSON, err := json.Marshal(updatedNode)
			if err != nil {
				r.Log.Error(err, "Uninstall: Unable to remove the NodeAffinityKey from the Node.", "Node", node.Name)
				continue
			}

			patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
			if err != nil {
				r.Log.Error(err, "Uninstall: Unable to remove the NodeAffinityKey from the Node.", "Node", node.Name)
				continue
			}

			err = r.Client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
			if err != nil {
				r.Log.Error(err, "Uninstall: Unable to remove the NodeAffinityKey from the Node.", "Node", node.Name)
				continue
			}

		}

	}
	return nil
}

// deleteNodeTaint deletes the default NodeTolerationKey from the OCS nodes
func (r *StorageClusterReconciler) deleteNodeTaint(sc *ocsv1.StorageCluster) (err error) {

	nodes, err := r.getStorageClusterEligibleNodes(sc)
	if err != nil {
		r.Log.Error(err, "Uninstall: Unable to obtain the list of nodes eligible for the Storage Cluster.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name)) //nolint:gosimple
		return nil
	}
	for _, node := range nodes.Items {
		r.Log.Info("Uninstall: Deleting OCS NodeTolerationKey from the Node.", "Node", node.Name)
		updatedNode := node.DeepCopy()
		updatedNode.Spec.Taints = make([]corev1.Taint, 0)
		for _, taint := range node.Spec.Taints {
			if defaults.NodeTolerationKey == taint.Key {
				continue
			}
			updatedNode.Spec.Taints = append(updatedNode.Spec.Taints, taint)
		}

		oldJSON, err := json.Marshal(node)
		if err != nil {
			r.Log.Error(err, "Uninstall: Unable to remove the NodeTolerationKey from the Node.", "Node", node.Name)
			continue
		}

		newJSON, err := json.Marshal(updatedNode)
		if err != nil {
			r.Log.Error(err, "Uninstall: Unable to remove the NodeTolerationKey from the Node.", "Node", node.Name)
			continue
		}

		patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
		if err != nil {
			r.Log.Error(err, "Uninstall: Unable to remove the NodeTolerationKey from the Node.", "Node", node.Name)
			continue
		}

		err = r.Client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			r.Log.Error(err, "Uninstall: Unable to remove the NodeTolerationKey from the Node.", "Node", node.Name)
			continue
		}

	}

	return nil
}

// setRookUninstallandCleanupPolicy sets the uninstall mode and cleanup policy for rook based on the annotation on the StorageCluster
func (r *StorageClusterReconciler) setRookUninstallandCleanupPolicy(instance *ocsv1.StorageCluster, cephCluster *cephv1.CephCluster) (err error) {

	var updateRequired bool

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
		if (v == string(UninstallModeForced)) && (!cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes) {
			cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes = true
			updateRequired = true
		} else if (v == string(UninstallModeGraceful)) && (cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes) {
			cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes = false
			updateRequired = true
		}
	}

	if updateRequired {
		err := r.Client.Update(context.TODO(), cephCluster)
		if err != nil {
			return fmt.Errorf("Uninstall: Unable to update the cephCluster to set uninstall mode and/or cleanup policy: %v", err)
		}
		r.Log.Info("Uninstall: CephCluster uninstall mode and cleanup policy has been set.", "CephCluser", klog.KRef(cephCluster.Namespace, cephCluster.Name))
	}

	return nil
}

// setNoobaaUninstallMode sets the uninstall mode for Noobaa based on the annotation on the StorageCluster
func (r *StorageClusterReconciler) setNoobaaUninstallMode(sc *ocsv1.StorageCluster) error {
	// Do this if Noobaa is being managed by the OCS operator
	if sc.Spec.MultiCloudGateway != nil {
		reconcileStrategy := ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyIgnore {
			return nil
		}
	}
	noobaa := &nbv1.NooBaa{}
	var updateRequired bool

	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: NooBaa not found, can't set uninstall mode.", "Noobaa", klog.KRef(sc.Namespace, "noobaa"))
			return nil
		}
		return fmt.Errorf("Uninstall: Error while getting NooBaa %v", err)
	}

	// Explicitly allow deletion of NooBaa CR
	if !noobaa.Spec.CleanupPolicy.AllowNoobaaDeletion {
		noobaa.Spec.CleanupPolicy.AllowNoobaaDeletion = true
		updateRequired = true
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
		err = r.Client.Update(context.TODO(), noobaa)
		if err != nil {
			return fmt.Errorf("Uninstall: Unable to update NooBaa uninstall mode: %v", err)
		}
		r.Log.Info("Uninstall: NooBaa uninstall mode has been set.", "NooBaa", klog.KRef(noobaa.Namespace, noobaa.Name))
	}

	return nil
}

// checkAndSetUninstallAnnotations looks at the current uninstall  & cleanup annotations on the StorageCluster and sets defaults if none or unrecognized ones are set.
// It returns true if storageCluster needs to be updated else returns false
func (r *StorageClusterReconciler) checkAndSetUninstallAnnotations(sc *ocsv1.StorageCluster) bool {
	var updateRequired bool

	if v, found := sc.ObjectMeta.Annotations[UninstallModeAnnotation]; !found {
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(UninstallModeAnnotation), string(UninstallModeGraceful))
		r.Log.Info("Uninstall: Setting uninstall mode annotation to default.", "UninstallMode", UninstallModeGraceful)
		updateRequired = true
	} else if found && v != string(UninstallModeGraceful) && v != string(UninstallModeForced) {
		// if wrong value found
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(UninstallModeAnnotation), string(UninstallModeGraceful))
		r.Log.Info("Uninstall: Found unrecognized uninstall mode annotation. Changing it to default.",
			"CurrentUninstallMode", v, "DefaultUninstallMode", UninstallModeGraceful)
		updateRequired = true
	}

	if v, found := sc.ObjectMeta.Annotations[CleanupPolicyAnnotation]; !found {
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(CleanupPolicyAnnotation), string(CleanupPolicyDelete))
		r.Log.Info("Uninstall: Setting uninstall cleanup policy annotation to default.", "CleanupPolicy", CleanupPolicyDelete)
		updateRequired = true
	} else if found && v != string(CleanupPolicyDelete) && v != string(CleanupPolicyRetain) {
		// if wrong value found
		metav1.SetMetaDataAnnotation(&sc.ObjectMeta, string(CleanupPolicyAnnotation), string(CleanupPolicyDelete))
		r.Log.Info("Uninstall: Found unrecognized uninstall cleanup policy annotation.Changing it to default.",
			"CurrentCleanupPolicy", v, "DefaultCleanupPolicy", CleanupPolicyDelete)
		updateRequired = true
	}

	if updateRequired {
		return true
	}
	return false
}

// verifyNoStorageConsumerExist verifies there are no storageConsumers on the same namespace
func (r *StorageClusterReconciler) verifyNoStorageConsumerExist(instance *ocsv1.StorageCluster) error {

	storageConsumers := &ocsv1alpha1.StorageConsumerList{}
	err := r.Client.List(context.TODO(), storageConsumers, &client.ListOptions{Namespace: instance.Namespace})
	if err != nil {
		return err
	}

	if len(storageConsumers.Items) != 0 {
		err = fmt.Errorf("Failed to cleanup provider resources. StorageConsumers are present in the %s namespace. "+
			"Offboard all consumer clusters for the provider cleanup to proceed", instance.Namespace)
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, "ProviderCleanup", err.Error())
		r.Log.Error(err, "Waiting for all consumer clusters to offboard.")
		return err
	}

	return nil
}

// deleteResources is the function where the storageClusterFinalizer is handled
// Every function that is called within this function should be idempotent
func (r *StorageClusterReconciler) deleteResources(sc *ocsv1.StorageCluster) (reconcile.Result, error) {

	cephCluster := &cephv1.CephCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: util.GenerateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	if !errors.IsNotFound(err) && cephCluster.GetDeletionTimestamp().IsZero() {
		err = r.setRookUninstallandCleanupPolicy(sc, cephCluster)
		if err != nil {
			return reconcile.Result{}, err
		}
		err = r.Client.Delete(context.TODO(), cephCluster)
		if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

	}

	err = r.setNoobaaUninstallMode(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	// the metric exporter creation flow does not comply with the ensureCreatedPattern,
	// so we don't have a way to add ensureDeleted until a refactor
	// What's unique: cephcluster waits for all cephclients to deleted
	// and this cephclient wait for storagecluster to cleaned up,
	// so ownerreference flow does not work here, and we need a explicit deletion
	err = r.deleteMetricsExporterCephClient(sc.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	objs := []resourceManager{
		&ocsExternalResources{},
		&ocsNoobaaSystem{},
		&storageClient{},
		&storageConsumer{},
		&ocsProviderServer{},
		&obcStorageClasses{},
		&ocsCephRGWRoutes{},
		&ocsConsoleConfiguration{},
		&ocsCephObjectStoreUsers{},
		&ocsCephObjectStores{},
		&ocsCephRbdMirrors{},
		&ocsCephNFS{},
		&ocsCephNFSService{},
		&ocsCephFilesystems{},
		&ocsCephBlockPools{},
		&ocsSnapshotClass{},
		&ocsStorageQuota{},
		&ocsCephCluster{},
		&backingStorageClasses{},
		&odfInfoConfig{},
	}

	for _, obj := range objs {
		res, err := obj.ensureDeleted(r, sc)
		if err != nil {
			return reconcile.Result{}, err
		} else if !res.IsZero() {
			return res, nil
		}
	}

	err = r.deleteNodeTaint(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = deleteKMSResources(r, sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.deleteExternalSecret(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	// TODO: skip the deletion of these labels till we figure out a way to wait
	// for the cleanup jobs
	//err = r.deleteNodeAffinityKeyFromNodes(sc)
	//if err != nil {
	//	return err
	//}

	return reconcile.Result{}, nil
}
