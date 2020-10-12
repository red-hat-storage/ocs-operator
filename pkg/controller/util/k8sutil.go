package util

import (
	"fmt"
	"os"
)

// WatchNamespaceEnvVar is the constant for env variable WATCH_NAMESPACE
// which is the namespace where the watch activity happens.
// this value is empty if the operator is running with clusterScope.
const WatchNamespaceEnvVar = "WATCH_NAMESPACE"

// GetWatchNamespace returns the namespace the operator should be watching for changes
func GetWatchNamespace() (string, error) {
	ns, found := os.LookupEnv(WatchNamespaceEnvVar)
	if !found {
		return "", fmt.Errorf("%s must be set", WatchNamespaceEnvVar)
	}
	return ns, nil
}
