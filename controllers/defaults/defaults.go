// Package defaults contains the default values for various configurable
// options of a StorageCluster
package defaults

const (
	// NodeAffinityKey is the node label to determine which nodes belong
	// to a storage cluster
	NodeAffinityKey = "cluster.ocs.openshift.io/openshift-storage"
	// NodeTolerationKey is the taint all OCS Pods should tolerate
	NodeTolerationKey = "node.ocs.openshift.io/storage"
	// RackTopologyKey is the node label used to distribute storage nodes
	// when there are not enough AZs presnet across the nodes
	RackTopologyKey = "topology.rook.io/rack"
	// KubeMajorTopologySpreadConstraints is the minimum major kube version to support TSC
	// used along with KubeMinorTSC for version comparison
	KubeMajorTopologySpreadConstraints = "1"
	// KubeMinorTopologySpreadConstraints is the minimum minor kube version to support TSC
	// used along with KubeMajorTSC for version comparison
	KubeMinorTopologySpreadConstraints = "19"
)

var (
	// DefaultMgrCount is the default number of Ceph Manager Pods
	DefaultMgrCount = 1
	// ArbiterModeMgrCount is the number of Ceph Manager pods in arbiter mode
	ArbiterModeMgrCount = 2
	// DefaultMonCount is the number of monitors to be configured for the CephCluster
	DefaultMonCount = 3
	// ArbiterModeMonCount is the number of monitors to be configured for the CephCluster in arbiter mode
	ArbiterModeMonCount = 5
	// DeviceSetReplica is the default number of Rook-Ceph
	// StorageClassDeviceSets per StorageCluster StorageDeviceSet
	// This is equal to the default number of failure domains for OSDs
	DeviceSetReplica = 3
	// CephObjectStoreGatewayInstances is the default number of RGW instances to create
	CephObjectStoreGatewayInstances = 1
	// ArbiterCephObjectStoreGatewayInstances is the default number of RGW instances to create when arbiter is enabled
	ArbiterCephObjectStoreGatewayInstances = 2
	// IsUnsupportedCephVersionAllowed is a string that determines if the CephCluster should allow unsupported ceph version image
	IsUnsupportedCephVersionAllowed = ""
	// ArbiterModeDeviceSetReplica is the default number of Rook-Ceph
	// StorageClassDeviceSets per StorageCluster StorageDeviceSet when arbiter is enabled
	// This is equal to the default number of failure domains for OSDs when arbiter is enabled
	ArbiterModeDeviceSetReplica = 2
	// ReplicasPerFailureDomain is the default replica count in the failure domain
	// This maps to the ReplicasPerFailureDomain in the CephReplicatedSpec when creating the CephBlockPools
	ReplicasPerFailureDomain = 1
	// ArbiterReplicasPerFailureDomain is the default replica count in the failure domain when arbiter is enabled
	// This maps to the ReplicasPerFailureDomain in the CephReplicatedSpec when creating the CephBlockPools
	ArbiterReplicasPerFailureDomain = 2
)
