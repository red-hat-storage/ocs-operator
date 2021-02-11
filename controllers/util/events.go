package util

const (
	// EventTypeWarning is used to alert user of failures that require user action
	EventTypeWarning = "Warning"

	// EventReasonValidationFailed is used when the StorageCluster spec validation fails
	EventReasonValidationFailed = "FailedValidation"

	// EventReasonUninstallPending is used when the StorageCluster uninstall is Pending
	EventReasonUninstallPending = "UninstallPending"
)
