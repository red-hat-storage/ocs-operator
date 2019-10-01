// Package defaults contains the default values for various configurable
// options of a StorageCluster
package defaults

const (
	nodeAffinityKey   = "cluster.ocs.openshift.io/openshift-storage"
	nodeTolerationKey = "node.ocs.openshift.io/storage"
)

var (
	//MonCount is the default number of monitors to be configured for the CephCluster
	MonCount = 3
)
