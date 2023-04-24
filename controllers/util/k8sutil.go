package util

import (
	"fmt"
	"os"
	"strings"
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
