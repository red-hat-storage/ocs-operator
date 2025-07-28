package defaults

import (
	"runtime"
	"strconv"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// GetDaemonResources returns a custom ResourceRequirements for the passed
// name, if found in the passed resource map. If not, it returns the default
// value for the given name.
func GetDaemonResources(name string, custom map[string]corev1.ResourceRequirements) corev1.ResourceRequirements {
	if res, ok := custom[name]; ok {
		return res
	}
	resourceRequirements := DaemonResources[name]
	if runtime.GOARCH == "s390x" {
		// Adjust CPU requests to half for IBM Z platform
		resourceRequirementsCopy := resourceRequirements.DeepCopy()
		if resourceRequirementsCopy.Requests != nil {
			if cpuRequest, exists := resourceRequirementsCopy.Requests[corev1.ResourceCPU]; exists {
				resourceRequirementsCopy.Requests[corev1.ResourceCPU] = halveCpuResource(cpuRequest)
			}
		}
		return *resourceRequirementsCopy
	}
	return resourceRequirements
}

func GetProfileDaemonResources(name string, sc *ocsv1.StorageCluster) corev1.ResourceRequirements {
	customResourceRequirements := sc.Spec.Resources
	if res, ok := customResourceRequirements[name]; ok {
		return res
	}
	resourceProfile := sc.Spec.ResourceProfile
	resourceProfile = strings.ToLower(resourceProfile)
	var resourceRequirements corev1.ResourceRequirements
	switch resourceProfile {
	case "lean":
		resourceRequirements = LeanDaemonResources[name]
	case "balanced":
		resourceRequirements = BalancedDaemonResources[name]
	case "performance":
		resourceRequirements = PerformanceDaemonResources[name]
	default:
		resourceRequirements = BalancedDaemonResources[name]
	}
	if runtime.GOARCH == "s390x" {
		// Adjust CPU requests to half for IBM Z platform
		resourceRequirementsCopy := resourceRequirements.DeepCopy()
		if resourceRequirementsCopy.Requests != nil {
			if cpuRequest, exists := resourceRequirementsCopy.Requests[corev1.ResourceCPU]; exists {
				resourceRequirementsCopy.Requests[corev1.ResourceCPU] = halveCpuResource(cpuRequest)
			}
		}
		return *resourceRequirementsCopy
	}
	return resourceRequirements
}

func halveCpuResource(cpuQty resource.Quantity) resource.Quantity {
	str := strconv.FormatInt(cpuQty.MilliValue()/2, 10) + "m"
	return resource.MustParse(str)
}
