package collectors

// rookGenericPhases enumerates every generic Rook status.phase string value a CR can
// report, in the same order as the numeric codes returned by rookGenericStatusCode.
// Shared by every CR collector whose status.phase is a plain string using Rook's
// common phase vocabulary (github.com/rook/rook/pkg/operator/k8sutil's
// Ready/Failed/Reconciling/ReconcileFailed status constants): CephNFS,
// CephNVMeOFGateway, CephRBDMirror, CephObjectStoreUser, ... so the phase-to-code
// mapping (and any phases added to it later) stays identical across all of them.
var rookGenericPhases = []string{
	"",
	"Ready",
	"Failed",
	"Reconciling",
	"ReconcileFailed",
}

// rookGenericStatusCode maps a generic Rook status.phase string to its numeric
// status_phase gauge value: 0=Empty, 1=Ready, 2=Failed, 3=Reconciling,
// 4=ReconcileFailed. ok is false if phase doesn't match any known value, in which
// case the caller should skip emitting the metric (and log the unexpected value
// itself, since it has the resource name/namespace for context).
func rookGenericStatusCode(phase string) (code float64, ok bool) {
	switch phase {
	case "":
		return 0, true
	case "Ready":
		return 1, true
	case "Failed":
		return 2, true
	case "Reconciling":
		return 3, true
	case "ReconcileFailed":
		return 4, true
	default:
		return 0, false
	}
}
