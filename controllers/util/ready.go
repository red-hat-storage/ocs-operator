package util

import (
	"os"
)

// FileName represent the full path to the file for determining ready status
const FileName = "/tmp/operator-sdk-ready"

// Ready holds state about whether the operator is ready and communicates that
// to a Kubernetes readiness probe.
type Ready interface {
	// Set ensures that future readiness probes will indicate that the operator
	// is ready.
	Set() error

	// Unset ensures that future readiness probes will indicate that the
	// operator is not ready.
	Unset() error
}

// NewFileReady returns a Ready that uses the presence of a file on disk to
// communicate whether the operator is ready. The operator's Pod definition
// should include a readinessProbe of "exec" type that calls
// "stat /tmp/operator-sdk-ready".
func NewFileReady() Ready {
	return fileReady{}
}

type fileReady struct{}

// Set creates a file on disk whose presence can be used by a readiness probe
// to determine that the operator is ready.
func (r fileReady) Set() error {
	f, err := os.Create(FileName)
	if err != nil {
		if os.IsExist(err) {
			return nil
		}
		return err
	}
	return f.Close()
}

// Unset removes the file on disk that was created by Set().
func (r fileReady) Unset() error {
	if _, err := os.Stat(FileName); os.IsNotExist(err) {
		return nil
	}
	return os.Remove(FileName)
}
