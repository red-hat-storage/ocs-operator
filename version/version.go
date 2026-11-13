package version

import (
	"fmt"

	"github.com/blang/semver/v4"
)

var (
	// Version of the operator (single source of truth)
	Version = "5.1.0"
)

// Returns Major and Minor Version joined by dot (.)
func GetMajorAndMinorVersion() (string, error) {
	semverVersion, err := semver.Parse(Version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", semverVersion.Major, semverVersion.Minor), nil
}
