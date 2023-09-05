package util

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which is the namespace where the watch activity happens.
	// this value is empty if the operator is running with clusterScope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"

	// SingleNodeEnvVar is set if StorageCluster needs to be deployed on a single node
	SingleNodeEnvVar = "SINGLE_NODE"

	// This configmap is purely for the OCS operator to use.
	OcsOperatorConfigName = "ocs-operator-config"

	// This configmap is watched by rook-ceph-operator & is reserved only for manual overrides.
	RookCephOperatorConfigName = "rook-ceph-operator-config"

	// These are the keys in the ocs-operator-config configmap
	ClusterNameKey              = "CSI_CLUSTER_NAME"
	EnableReadAffinityKey       = "CSI_ENABLE_READ_AFFINITY"
	CephFSKernelMountOptionsKey = "CSI_CEPHFS_KERNEL_MOUNT_OPTIONS"
	EnableTopologyKey           = "CSI_ENABLE_TOPOLOGY"
	TopologyDomainLabelsKey     = "CSI_TOPOLOGY_DOMAIN_LABELS"
	EnableNFSKey                = "ROOK_CSI_ENABLE_NFS"
)

// GetWatchNamespace returns the namespace the operator should be watching for changes
func GetWatchNamespace() (string, error) {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}
	return ns, nil
}

// OperatorNamespaceEnvVar is the constant for env variable OPERATOR_NAMESPACE
// which is the namespace where operator pod is deployed.
const OperatorNamespaceEnvVar = "OPERATOR_NAMESPACE"

// GetOperatorNamespace returns the namespace where the operator is deployed.
func GetOperatorNamespace() (string, error) {
	ns, found := os.LookupEnv(OperatorNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", OperatorNamespaceEnvVar)
	}
	return ns, nil
}

// IsSingleNodeDeployment returns true if StorageCluster needs to be deployed on a single node.
func IsSingleNodeDeployment() bool {
	isSingleNode := os.Getenv(SingleNodeEnvVar)
	return strings.ToLower(strings.TrimSpace(isSingleNode)) == "true"
}

// getClusterID returns the cluster ID of the OCP-Cluster
func GetClusterID(ctx context.Context, kubeClient client.Client, logger *logr.Logger) string {
	clusterVersion := &configv1.ClusterVersion{}
	err := kubeClient.Get(ctx, types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		logger.Error(err, "Failed to get the clusterVersion version of the OCP cluster")
		return ""
	}
	return fmt.Sprint(clusterVersion.Spec.ClusterID)
}

// RestartPod restarts the pod with the given name in the given namespace by deleting it and letting another one be created
func RestartPod(ctx context.Context, kubeClient client.Client, logger *logr.Logger, name string, namespace string) {
	logger.Info("restarting pod", "name", name, "namespace", namespace)
	podList := &corev1.PodList{}
	err := kubeClient.List(ctx, podList, client.InNamespace(namespace))
	if err != nil {
		logger.Error(err, "failed to list pods", "namespace", namespace)
		return
	}
	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, name) {
			err = kubeClient.Delete(ctx, &pod)
			if err != nil {
				logger.Error(err, "failed to delete pod", "name", pod.Name, "namespace", namespace)
			}
		}
	}
}
