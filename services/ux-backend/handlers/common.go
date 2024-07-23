package handlers

import "os"

const (
	ContentTypeTextPlain = "text/plain"
)

var namespace string

// returns namespace found in pod
func GetPodNamespace() string {
	if namespace != "" {
		return namespace
	}
	if ns := os.Getenv("OPERATOR_NAMESPACE"); ns != "" {
		namespace = ns
		return namespace
	}
	panic("Value for env var 'POD_NAMESPACE' is empty")
}
