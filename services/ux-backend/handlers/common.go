package handlers

import (
	"os"
)

const (
	ContentTypeTextPlain                  = "text/plain"
	RookCephResourceForceDeleteAnnotation = "ocs.openshift.io.storageconsumer/force-deletion"
)

var namespace string

// returns namespace found in env value, will panic if value is empty
func GetPodNamespace() string {
	if namespace != "" {
		return namespace
	}
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		namespace = ns
		return namespace
	}
	panic("Value for env var 'POD_NAMESPACE' is empty")
}
