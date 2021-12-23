package common

// ExternalResource is connections details required to connect to the external cluster
// TODO:  This is duplicate of the existing `ExternalResource` in `storagecluster/external_resource.go` file.
// Need to duplicate it to prevent circular imports. This should be fixed.
type ExternalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}
