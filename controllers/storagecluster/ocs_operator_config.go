package storagecluster

import (
	"reflect"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

func (r *StorageClusterReconciler) ensureOCSOperatorConfig(sc *ocsv1.StorageCluster) error {
	ocsOperatorConfigData := map[string]string{
		util.ClusterNameKey:              util.GetClusterID(r.ctx, r.Client, &r.Log),
		util.EnableReadAffinityKey:       strconv.FormatBool(!sc.Spec.ExternalStorage.Enable),
		util.CephFSKernelMountOptionsKey: getCephFSKernelMountOptions(sc),
		util.EnableTopologyKey:           strconv.FormatBool(sc.Spec.ManagedResources.CephNonResilientPools.Enable),
		util.TopologyDomainLabelsKey:     getFailureDomainKey(sc),
		util.EnableNFSKey:                getEnableNFSVal(sc),
	}

	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.OcsOperatorConfigName,
			Namespace: sc.Namespace,
		},
		Data: ocsOperatorConfigData,
	}

	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, cm, func() error {

		// This configmap was created and controlled by the OCSInitialization earlier.
		// We are required to remove OCSInitialization as a controller before adding storageCluster as controller.
		if existing := metav1.GetControllerOfNoCopy(cm); existing != nil && existing.Kind == "OCSInitialization" {
			existing.BlockOwnerDeletion = nil
			existing.Controller = nil
		}
		if !reflect.DeepEqual(cm.Data, ocsOperatorConfigData) {
			r.Log.Info("Updating ocs-operator-config configmap")
			cm.Data = ocsOperatorConfigData
		}
		return ctrl.SetControllerReference(sc, cm, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update ocs-operator-config configmap", "OperationResult", opResult)
		return err
	}
	// If configmap is created or updated, restart the rook-ceph-operator pod to pick up the new change
	if opResult == controllerutil.OperationResultCreated || opResult == controllerutil.OperationResultUpdated {
		r.Log.Info("ocs-operator-config configmap created/updated. Restarting rook-ceph-operator pod to pick up the new values")
		util.RestartPod(r.ctx, r.Client, &r.Log, "rook-ceph-operator", sc.Namespace)
	}

	return nil
}

// getCephFSKernelMountOptions returns the kernel mount options for CephFS based on the spec on the StorageCluster
func getCephFSKernelMountOptions(sc *ocsv1.StorageCluster) string {
	// If Encryption is enabled, Always use secure mode
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil &&
		sc.Spec.Network.Connections.Encryption != nil && sc.Spec.Network.Connections.Encryption.Enabled {
		return "ms_mode=secure"
	}

	// If Encryption is not enabled, but Compression or RequireMsgr2 is enabled, use prefer-crc mode
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil &&
		((sc.Spec.Network.Connections.Compression != nil && sc.Spec.Network.Connections.Compression.Enabled) ||
			sc.Spec.Network.Connections.RequireMsgr2) {
		return "ms_mode=prefer-crc"
	}

	// Network spec always has higher precedence even in the External or Provider cluster. so they are checked first above

	// None of Encryption, Compression, RequireMsgr2 are enabled on the StorageCluster
	// If it's an External or Provider cluster, We don't require msgr2 by default so no mount options are needed
	if sc.Spec.ExternalStorage.Enable || sc.Spec.AllowRemoteStorageConsumers {
		return "ms_mode=legacy"
	}
	// If none of the above cases apply, We set RequireMsgr2 true by default on the cephcluster
	// so we need to set the mount options to prefer-crc
	return "ms_mode=prefer-crc"
}

// getEnableNFSVal returns the value of enableNFS based on the spec on the StorageCluster
func getEnableNFSVal(sc *ocsv1.StorageCluster) string {
	if sc.Spec.NFS != nil && sc.Spec.NFS.Enable {
		return "true"
	}
	return "false"
}
