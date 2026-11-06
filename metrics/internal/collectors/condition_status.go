package collectors

import (
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

// cephConditionPhases enumerates every cephv1.ConditionType value a CR's status.phase
// can report. Shared by every CR collector whose status.phase is a cephv1.ConditionType
// (CephCluster, CephBlockPool, CephBlockPoolRadosNamespace, CephFilesystem,
// CephFilesystemSubVolumeGroup, CephObjectStore, ...), so the phase-to-code mapping (and
// any phases added to it later) stays identical across all of them.
var cephConditionPhases = []cephv1.ConditionType{
	cephv1.ConditionConnecting,
	cephv1.ConditionConnected,
	cephv1.ConditionProgressing,
	cephv1.ConditionReady,
	cephv1.ConditionFailure,
	cephv1.ConditionDeleting,
	cephv1.ConditionDeletionIsBlocked,
}

// cephConditionStatusCode maps a cephv1.ConditionType status.phase to its numeric
// status_phase gauge value: 0=Connecting, 1=Connected, 2=Failure, 3=Ready,
// 4=Progressing, 5=Deleting, 6=DeletionIsBlocked. Failure is pinned to code 2 to match
// the "failure" code used by rookGenericStatusCode and noobaaStatusCode, so every
// CR's PhaseFailure alert can check the same value regardless of which status
// vocabulary that CR uses. ok is false if phase doesn't match any known value, in
// which case the caller should skip emitting the metric (and log the unexpected
// value itself, since it has the resource name/namespace for context).
func cephConditionStatusCode(phase cephv1.ConditionType) (code float64, ok bool) {
	switch phase {
	case cephv1.ConditionConnecting:
		return 0, true
	case cephv1.ConditionConnected:
		return 1, true
	case cephv1.ConditionFailure:
		return 2, true
	case cephv1.ConditionReady:
		return 3, true
	case cephv1.ConditionProgressing:
		return 4, true
	case cephv1.ConditionDeleting:
		return 5, true
	case cephv1.ConditionDeletionIsBlocked:
		return 6, true
	default:
		return 0, false
	}
}
