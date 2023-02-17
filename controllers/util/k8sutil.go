package util

import (
	"context"
	"fmt"
	"os"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
	// which is the namespace where the watch activity happens.
	// this value is empty if the operator is running with clusterScope.
	WatchNamespaceEnvVar = "WATCH_NAMESPACE"

	// This configmap is purely for the OCS operator to use.
	OcsOperatorConfigName = "ocs-operator-config"
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

// RestartRookOperatorPod restarts the rook-operator pod in the OCP cluster
func RestartRookOperatorPod(ctx context.Context, kubeClient client.Client, logger *logr.Logger, namespace string) {
	podList := &corev1.PodList{}
	err := kubeClient.List(ctx, podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rook-ceph-operator"})
	if err != nil {
		logger.Error(err, "Failed to list rook-ceph-operator pod")
		return
	}
	for _, pod := range podList.Items {
		err := kubeClient.Delete(ctx, &pod)
		if err != nil {
			logger.Error(err, "Failed to delete rook-ceph-operator pod")
			return
		}
	}
}
