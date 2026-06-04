/*
Copyright The CloudNativePG Contributors

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package version

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

var semanticVersionRegex = regexp.MustCompile(`^(\d\.?)+`)

// Data is a structure used to manage a PostgreSQL version,
// consisting of two parts: major and minor. For instance,
// version 10.0 corresponds to a major of 10 and a minor of 0.
// See: https://www.postgresql.org/support/versioning/
type Data struct {
	major uint64
	minor uint64
}

// Major gets the major version (i.e. 10)
func (d Data) Major() uint64 {
	return d.major
}

// Minor gets the minor version (i.e. 0)
func (d Data) Minor() uint64 {
	return d.minor
}

// Less is the implementation of the "less than" operator
func (d Data) Less(other Data) bool {
	if d.major == other.major {
		return d.minor < other.minor
	}
	return d.major < other.major
}

// New constructs a version from its components
func New(major, minor uint64) Data {
	return Data{
		major: major,
		minor: minor,
	}
}

// FromTag parse a PostgreSQL version string returning
// a major version ID. Example:
//
//	FromTag("11.2") == (11,2)
//	FromTag("12.1") == (12,1)
//	FromTag("13.3.2.1-1") == (13,3)
//	FromTag("13.4") == (13,4)
//	FromTag("14") == (14,0)
//	FromTag("15.5-10") == (15,5)
//	FromTag("16.0") == (16,0)
//	FromTag("17beta1") == (17,0)
//	FromTag("17rc1") == (17,0)
func FromTag(version string) (Data, error) {
	if !semanticVersionRegex.MatchString(version) {
		return Data{},
			fmt.Errorf("version not starting with a semantic version regex (%v): %s", semanticVersionRegex, version)
	}

	if versionOnly := semanticVersionRegex.FindString(version); versionOnly != "" {
		version = versionOnly
	}

	splitVersion := strings.Split(version, ".")

	majorVersion, err := strconv.Atoi(splitVersion[0])
	if err != nil {
		return Data{}, fmt.Errorf("wrong PostgreSQL major in version %v", version)
	}

	minorVersion := 0
	if len(splitVersion) > 1 {
		minorVersion, err = strconv.Atoi(splitVersion[1])
		if err != nil {
			return Data{}, fmt.Errorf("wrong PostgreSQL minor in version %v", version)
		}
	}

	if majorVersion < 0 || majorVersion > math.MaxInt {
		return Data{}, fmt.Errorf("wrong PostgreSQL major in version %v", version)
	}

	if minorVersion < 0 || minorVersion > math.MaxInt {
		return Data{}, fmt.Errorf("wrong PostgreSQL minor in version %v", version)
	}

	return Data{
		major: uint64(majorVersion),
		minor: uint64(minorVersion),
	}, nil
}

// IsUpgradePossible detect if it's possible to upgrade from fromVersion to
// toVersion
func IsUpgradePossible(fromVersion, toVersion Data) bool {
	return fromVersion.major == toVersion.major
}
