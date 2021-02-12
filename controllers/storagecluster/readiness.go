package storagecluster

import (
	"errors"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

var ready bool

var ReadinessChecker healthz.Checker = func(_ *http.Request) error {
	if ready {
		return nil
	}
	return errors.New("StorageCluster is not ready yet")
}

func ReadinessSet() {
	ready = true
}

func ReadinessUnset() {
	ready = false
}
