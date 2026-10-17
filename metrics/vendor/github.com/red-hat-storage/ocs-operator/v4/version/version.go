package version

import (
	"fmt"

	"github.com/blang/semver/v4"
)

var (
	// Version of the operator
	Version = "4.18.0"
)

// GetMajorAndMinorVersion returns the operator's major and minor version joined by a dot.
func GetMajorAndMinorVersion() (string, error) {
	semverVersion, err := semver.Parse(Version)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%d.%d", semverVersion.Major, semverVersion.Minor), nil
}
