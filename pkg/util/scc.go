package util

import (
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	secv1 "github.com/openshift/api/security/v1"
)

const (
	// OpenShift built-in SCCs used by low-privilege ODF workloads.
	SCCRestrictedV2   = "restricted-v2"
	SCCHostNetworkV2  = "hostnetwork-v2"
	SCCNonRootV2      = "nonroot-v2"

	// ODF custom SCCs created or referenced by ocs-operator.
	SCCRookCeph    = "rook-ceph"
	SCCRookCephCSI = "rook-ceph-csi"
	SCCODFBlackbox = "odf-blackbox-scc"
)

// RequiredSCCForHostNetwork returns the built-in SCC for pods that may run with host networking.
func RequiredSCCForHostNetwork(hostNetwork bool) string {
	if hostNetwork {
		return SCCHostNetworkV2
	}
	return SCCRestrictedV2
}

func RequiredSCCAnnotation(sccName string) map[string]string {
	return map[string]string{
		secv1.RequiredSCCAnnotation: sccName,
	}
}

// MergePodTemplateAnnotations copies existing pod-template annotations and sets
// openshift.io/required-scc without overwriting unrelated keys.
func MergePodTemplateAnnotations(existing map[string]string, sccName string) map[string]string {
	merged := map[string]string{}
	for k, v := range existing {
		merged[k] = v
	}
	merged[secv1.RequiredSCCAnnotation] = sccName
	return merged
}

// RookCephDaemonRequiredSCCAnnotations returns AnnotationsSpec entries for Ceph daemons
// that use the rook-ceph SCC. Do not use KeyAll — CSI pods require rook-ceph-csi.
func RookCephDaemonRequiredSCCAnnotations() map[string]map[string]string {
	scc := RequiredSCCAnnotation(SCCRookCeph)
	return map[string]map[string]string{
		"mon":             scc,
		"mgr":             scc,
		"osd":             scc,
		"prepareosd":      scc,
		"crashcollector":  scc,
		"exporter":        scc,
		"cleanup":         scc,
		"keyrotation":     scc,
		"cmdreporter":     scc,
		"mds":             scc,
		"rgw":             scc,
		"arbiter":         scc,
		"dashboard":       scc,
		"monitoring":      scc,
		"clusterMetadata": scc,
	}
}

// ToRookAnnotationsSpec converts string-keyed annotation maps to Rook AnnotationsSpec.
func ToRookAnnotationsSpec(annotations map[string]map[string]string) rookCephv1.AnnotationsSpec {
	spec := rookCephv1.AnnotationsSpec{}
	for key, value := range annotations {
		spec[rookCephv1.KeyType(key)] = value
	}
	return spec
}
