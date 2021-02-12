package storagecluster

import (
	"errors"
	"net/http"

	"sigs.k8s.io/controller-runtime/pkg/healthz"
)

var operatorReady bool

var ReadinessChecker healthz.Checker = func(_ *http.Request) error {
	if operatorReady {
		return nil
	}
	return errors.New("StorageCluster is not ready yet")
}

func ReadinessSet() {
	operatorReady = true
}

func ReadinessUnset() {
	operatorReady = false
}
