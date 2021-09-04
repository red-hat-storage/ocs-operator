package version

import (
	"github.com/red-hat-storage/ocs-operator/version"
)

// GetVersion returns the version of the exporter.
func GetVersion() string {
	// version of the Operator
	return version.Version
}
