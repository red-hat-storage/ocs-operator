package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
