package util

import (
	secv1 "github.com/openshift/api/security/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

const (
	// OpenShift built-in SCCs used by low-privilege ODF workloads.
	RestrictedV2SccName  = "restricted-v2"
	HostNetworkV2SccName = "hostnetwork-v2"
	NonRootV2SccName     = "nonroot-v2"

	// ODF custom SCCs created or referenced by ocs-operator.
	RookCephSccName    = "rook-ceph"
	OdfBlackboxSccName = "odf-blackbox-scc"
)

// GetRookCephDaemonSCCAnnotations returns AnnotationsSpec entries for Ceph daemons
// that use the rook-ceph SCC. Do not use KeyAll — CSI pods require rook-ceph-csi.
func GetRookCephDaemonSCCAnnotations() rookCephv1.AnnotationsSpec {
	scc := map[string]string{
		secv1.RequiredSCCAnnotation: RookCephSccName,
	}
	return rookCephv1.AnnotationsSpec{
		rookCephv1.KeyMon:             scc,
		rookCephv1.KeyMgr:             scc,
		rookCephv1.KeyOSD:             scc,
		rookCephv1.KeyOSDPrepare:      scc,
		rookCephv1.KeyCrashCollector:  scc,
		rookCephv1.KeyCephExporter:    scc,
		rookCephv1.KeyCleanup:         scc,
		rookCephv1.KeyRotation:        scc,
		rookCephv1.KeyCmdReporter:     scc,
		rookCephv1.KeyMds:             scc,
		rookCephv1.KeyRgw:             scc,
		rookCephv1.KeyMonArbiter:      scc,
		rookCephv1.KeyDashboard:       scc,
		rookCephv1.KeyMonitoring:      scc,
		rookCephv1.KeyClusterMetadata: scc,
	}
}
