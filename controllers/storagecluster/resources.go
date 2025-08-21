package storagecluster

import (
	"runtime"
	"strconv"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

const (
	IbmZCpuArch         = "s390x"
	IbmZCpuAdjustFactor = 0.5
)

// getDaemonResources returns ResourceRequirements for the given daemon name based on the storage cluster's resource profile.
// If any resource requirements are specified in the storage cluster, they are merged resource type wise with the profile defaults.
// If based on resource profile, no resources are specified, then fallback to daemon resources.
func getDaemonResources(name string, sc *ocsv1.StorageCluster) corev1.ResourceRequirements {
	var resourceRequirements corev1.ResourceRequirements
	var defaultResourceRequirements corev1.ResourceRequirements
	var profileResources map[string]corev1.ResourceRequirements
	switch strings.ToLower(sc.Spec.ResourceProfile) {
	case "lean":
		profileResources = defaults.LeanDaemonResources
	case "balanced":
		profileResources = defaults.BalancedDaemonResources
	case "performance":
		profileResources = defaults.PerformanceDaemonResources
	default:
		profileResources = defaults.BalancedDaemonResources
	}
	// Try to get resource requirements from the profiled resources map first
	if profileResource, found := profileResources[name]; found {
		defaultResourceRequirements = profileResource
	} else {
		// Fallback to plain daemon resources map if not found in profiled resources map
		defaultResourceRequirements = defaults.DaemonResources[name]
	}
	if runtime.GOARCH == IbmZCpuArch { // Adjust resources for IBM Z platform
		defaultResourceRequirements = adjustResource(defaultResourceRequirements, IbmZCpuAdjustFactor)
	}

	specifiedResourceRequirements, specified := sc.Spec.Resources[name]
	// if specified resource requirements is present but empty, the intention is to have no resource requirements
	if specified && !isResourceNonEmpty(specifiedResourceRequirements) {
		return specifiedResourceRequirements
	}
	// Resource specification for osd is handled at the deviceSet level
	if name == rookCephv1.ResourcesKeyOSD {
		specified = false
	}
	if specified {
		resourceRequirements = mergeResourceRequirements(defaultResourceRequirements, specifiedResourceRequirements)
	} else {
		resourceRequirements = defaultResourceRequirements
	}

	return resourceRequirements
}

// adjustResource multiplies the requests and limits by the adjustFactor
func adjustResource(resourceRequirements corev1.ResourceRequirements, adjustFactor float64) corev1.ResourceRequirements {
	resourceRequirementsCopy := resourceRequirements.DeepCopy()
	if resourceRequirementsCopy.Requests != nil {
		if cpuRequest, exists := resourceRequirementsCopy.Requests[corev1.ResourceCPU]; exists {
			resourceRequirementsCopy.Requests[corev1.ResourceCPU] = adjustCpuResource(cpuRequest, adjustFactor)
		}
	}
	if resourceRequirementsCopy.Limits != nil {
		if cpuLimit, exists := resourceRequirementsCopy.Limits[corev1.ResourceCPU]; exists {
			resourceRequirementsCopy.Limits[corev1.ResourceCPU] = adjustCpuResource(cpuLimit, adjustFactor)
		}
	}
	return *resourceRequirementsCopy
}

func adjustCpuResource(cpuQty resource.Quantity, adjustFactor float64) resource.Quantity {
	str := strconv.FormatInt(int64(float64(cpuQty.MilliValue())*adjustFactor), 10) + "m"
	return resource.MustParse(str)
}

func isResourceNonEmpty(resource corev1.ResourceRequirements) bool {
	return resource.Requests != nil || resource.Limits != nil
}

// mergeResourceRequirements merges specified resource requirements with defaults.
// If for a resource type(cpu, memory, storage) either requests or limits are specified,
// We only use the specified values for that type and don't add any defaults for that resource type.
// For other resource sections, we add the defaults for the resource type for that component.
// For example, if requests/limits only for CPU are specified, default limits & requests for memory would be added
func mergeResourceRequirements(defaultResourceRequirements corev1.ResourceRequirements, specifiedResourceRequirements corev1.ResourceRequirements) corev1.ResourceRequirements {
	var merged corev1.ResourceRequirements

	resources := []corev1.ResourceName{
		corev1.ResourceCPU,
		corev1.ResourceMemory,
		corev1.ResourceStorage,
		// corev1.ResourceEphemeralStorage, // Not used in OCS right now
	}

	for _, r := range resources {
		_, reqSpecified := specifiedResourceRequirements.Requests[r]
		_, limSpecified := specifiedResourceRequirements.Limits[r]

		switch {
		case reqSpecified && limSpecified:
			ensureRequestsInitialized(&merged)
			merged.Requests[r] = specifiedResourceRequirements.Requests[r]
			ensureLimitsInitialized(&merged)
			merged.Limits[r] = specifiedResourceRequirements.Limits[r]

		case reqSpecified:
			ensureRequestsInitialized(&merged)
			merged.Requests[r] = specifiedResourceRequirements.Requests[r]

		case limSpecified:
			ensureLimitsInitialized(&merged)
			merged.Limits[r] = specifiedResourceRequirements.Limits[r]

		default:
			if defReq, ok := defaultResourceRequirements.Requests[r]; ok {
				ensureRequestsInitialized(&merged)
				merged.Requests[r] = defReq
			}
			if defLim, ok := defaultResourceRequirements.Limits[r]; ok {
				ensureLimitsInitialized(&merged)
				merged.Limits[r] = defLim
			}
		}
	}

	return merged
}

func ensureRequestsInitialized(merged *corev1.ResourceRequirements) {
	if merged.Requests == nil {
		merged.Requests = make(corev1.ResourceList)
	}
}

// ensureLimitsInitialized initializes the Limits map if it's nil
func ensureLimitsInitialized(merged *corev1.ResourceRequirements) {
	if merged.Limits == nil {
		merged.Limits = make(corev1.ResourceList)
	}
}
