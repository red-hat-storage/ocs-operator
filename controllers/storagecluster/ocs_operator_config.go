package storagecluster

import (
	"context"
	"fmt"
	"strconv"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *StorageClusterReconciler) ensureOCSOperatorConfig(sc *ocsv1.StorageCluster) error {
	const (
		clusterNameKey              = "CSI_CLUSTER_NAME"
		enableReadAffinityKey       = "CSI_ENABLE_READ_AFFINITY"
		cephFSKernelMountOptionsKey = "CSI_CEPHFS_KERNEL_MOUNT_OPTIONS"
	)
	var (
		clusterNameVal             = r.getClusterID()
		enableReadAffinityVal      = strconv.FormatBool(!sc.Spec.ExternalStorage.Enable)
		cephFSKernelMountOptionVal = getCephFSKernelMountOptions(sc)
	)

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.OcsOperatorConfigName,
			Namespace: sc.Namespace,
		},
		Data: map[string]string{
			clusterNameKey:        clusterNameVal,
			enableReadAffinityKey: enableReadAffinityVal,
		},
	}

	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cm, func() error {

		// This configmap was created and controlled by the OCSInitialization earlier.
		// We are required to remove OCSInitialization as a controller before adding storageCluster as controller.
		if existing := metav1.GetControllerOfNoCopy(cm); existing != nil && existing.Kind == "OCSInitialization" {
			existing.BlockOwnerDeletion = nil
			existing.Controller = nil
		}

		if cm.Data[clusterNameKey] != clusterNameVal {
			cm.Data[clusterNameKey] = clusterNameVal
		}
		if cm.Data[enableReadAffinityKey] != enableReadAffinityVal {
			cm.Data[enableReadAffinityKey] = enableReadAffinityVal
		}
		if cm.Data[cephFSKernelMountOptionsKey] != cephFSKernelMountOptionVal {
			cm.Data[cephFSKernelMountOptionsKey] = cephFSKernelMountOptionVal
		}
		return ctrl.SetControllerReference(sc, cm, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, fmt.Sprintf("failed to update %q configmap", util.OcsOperatorConfigName))
		return err
	}
	// If configmap is created or updated, restart the rook-ceph-operator pod to pick up the new change
	if opResult == controllerutil.OperationResultCreated || opResult == controllerutil.OperationResultUpdated {
		r.restartRookCephOperatorPod(sc.Namespace)
		r.Log.Info(fmt.Sprintf("%q configmap updated & rook-ceph-operator pod restarted to pick up new values", util.OcsOperatorConfigName),
			"storageCluster", klog.KRef(sc.Namespace, sc.Name))
	}

	return nil
}

// restartRookOperatorPod restarts the rook-operator pod in the OCP cluster
func (r *StorageClusterReconciler) restartRookCephOperatorPod(namespace string) {
	podList := &corev1.PodList{}
	err := r.Client.List(context.TODO(), podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rook-ceph-operator"})
	if err != nil {
		r.Log.Error(err, "Failed to list rook-ceph-operator pod")
		return
	}
	for _, pod := range podList.Items {
		err := r.Client.Delete(context.TODO(), &pod)
		if err != nil {
			r.Log.Error(err, "Failed to delete rook-ceph-operator pod")
			return
		}
	}
}

// getClusterID returns the cluster ID of the OCP-Cluster
func (r *StorageClusterReconciler) getClusterID() string {
	clusterVersion := &configv1.ClusterVersion{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		r.Log.Error(err, "Failed to get the clusterVersion version of the OCP cluster")
		return ""
	}
	return fmt.Sprint(clusterVersion.Spec.ClusterID)
}

// getCephFSKernelMountOptions returns the kernel mount options for cephfs
func getCephFSKernelMountOptions(sc *ocsv1.StorageCluster) string {
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil && sc.Spec.Network.Connections.Encryption != nil && sc.Spec.Network.Connections.Encryption.Enabled {
		return "ms_mode=secure"
	}
	return "ms_mode=prefer-crc"
}
