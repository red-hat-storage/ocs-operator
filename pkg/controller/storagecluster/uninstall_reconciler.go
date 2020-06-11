package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
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
		reqLogger.Error(err, fmt.Sprintf("Unable to determine the StorageClass names"))
		return nil
	}
	for _, sc := range scs {
		existing := storagev1.StorageClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("StorageClass %s is already marked for deletion", existing.Name))
				break
			}

			reqLogger.Info(fmt.Sprintf("Deleting StorageClass %s", sc.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.client.Delete(context.TODO(), sc)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Ignoring error deleting the StorageClass %s", existing.Name))
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("StorageClass %s not found, nothing to do", sc.Name))
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
			reqLogger.Error(err, fmt.Sprintf("Unable to obtain the list of nodes eligible for the Storage Cluster"))
			return nil
		}
		for _, node := range nodes.Items {
			reqLogger.Info(fmt.Sprintf("Deleting OCS label from node %s", node.Name))
			new := node.DeepCopy()
			delete(new.ObjectMeta.Labels, defaults.NodeAffinityKey)

			oldJSON, err := json.Marshal(node)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			newJSON, err := json.Marshal(new)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeAffinityKey from the node %s", node.Name))
				continue
			}

			err = r.client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
			if err != nil {
				reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeAffinityKey from the node %s", node.Name))
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
		reqLogger.Error(err, fmt.Sprintf("Unable to obtain the list of nodes eligible for the Storage Cluster"))
		return nil
	}
	for _, node := range nodes.Items {
		reqLogger.Info(fmt.Sprintf("Deleting OCS NodeTolerationKey from the node %s", node.Name))
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
			reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		newJSON, err := json.Marshal(new)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

		err = r.client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		if err != nil {
			reqLogger.Error(err, fmt.Sprintf("Unable to remove the NodeTolerationKey from the node %s", node.Name))
			continue
		}

	}

	return nil
}
