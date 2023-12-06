package defaults

import (
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
)

// GetDaemonResources returns a custom ResourceRequirements for the passed
// name, if found in the passed resource map. If not, it returns the default
// value for the given name.
func GetDaemonResources(name string, custom map[string]corev1.ResourceRequirements) corev1.ResourceRequirements {
	if res, ok := custom[name]; ok {
		return res
	}
	return DaemonResources[name]
}
