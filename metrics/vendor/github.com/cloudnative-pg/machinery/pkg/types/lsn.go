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

package types

import (
	"fmt"
	"strconv"
	"strings"
)

// LSN is a string composed by two hexadecimal numbers, separated by "/"
type LSN string

// Less compares two LSNs
func (lsn LSN) Less(other LSN) bool {
	p1, err := lsn.Parse()
	if err != nil {
		p1 = 0
	}

	p2, err := other.Parse()
	if err != nil {
		p2 = 0
	}

	return p1 < p2
}

// Components an LSN into its components
func (lsn LSN) Components() (uint64, uint64, error) {
	components := strings.Split(string(lsn), "/")
	if len(components) != 2 {
		return 0, 0, fmt.Errorf("error parsing LSN %s", lsn)
	}

	// Segment is unsigned int 32, so we parse using 64 bits to avoid overflow on sign bit
	segment, err := strconv.ParseUint(components[0], 16, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing LSN %s: %w", lsn, err)
	}

	// Displacement is unsigned int 32, so we parse using 64 bits to avoid overflow on sign bit
	displacement, err := strconv.ParseUint(components[1], 16, 64)
	if err != nil {
		return 0, 0, fmt.Errorf("error parsing LSN %s: %w", lsn, err)
	}

	return segment, displacement, nil
}

// Parse an LSN in its components
func (lsn LSN) Parse() (uint64, error) {
	segment, displacement, err := lsn.Components()
	if err != nil {
		return 0, err
	}

	return (segment << 32) + displacement, nil
}

// WALFileName computes the name of the WAL file hosting the passed LSN
func (lsn LSN) WALFileName(tli int, walSegmentSize uint64) (string, error) {
	// Important: this implementation is based on the
	// XLogFileName and the XLogSegmentsPerXLogId PostgreSQL
	// functions

	value, err := lsn.Parse()
	if err != nil {
		return "", err
	}

	segmentNumber := value / walSegmentSize
	xlogSegmentsPerXLogID := 0x100000000 / walSegmentSize

	return fmt.Sprintf(
		"%08X%08X%08X",
		tli,
		segmentNumber/xlogSegmentsPerXLogID,
		segmentNumber%xlogSegmentsPerXLogID,
	), nil
}

// LSNStartFromWALName extracts the start LSN from a WAL file name
func LSNStartFromWALName(filename string, walSegSize uint64) (LSN, error) {
	if len(filename) != 24 {
		return "", fmt.Errorf("invalid WAL file name length")
	}

	var tli, log, segment uint64

	_, err := fmt.Sscanf(filename, "%08X%08X%08X", &tli, &log, &segment)
	if err != nil {
		fmt.Println("Error parsing string:", err)
		return "", err
	}

	xlogSegmentsPerXLogID := 0x100000000 / walSegSize

	logSegNo := log*xlogSegmentsPerXLogID + segment
	startLSN := logSegNo * walSegSize

	return Int64ToLSN(startLSN), nil
}

// WALFileStart computes the LSN corresponding to the WAL file start
func (lsn LSN) WALFileStart(walSegmentSize uint64) (LSN, error) {
	value, err := lsn.Parse()
	if err != nil {
		return "", err
	}

	segmentNumber := value / walSegmentSize
	trimmedValue := segmentNumber * walSegmentSize
	return Int64ToLSN(trimmedValue), nil
}

// Int64ToLSN convert an int64 LSN to its string representation
func Int64ToLSN(value uint64) LSN {
	return LSN(fmt.Sprintf("%X/%X", value>>32, value%0x100000000))
}
