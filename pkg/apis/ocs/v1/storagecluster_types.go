package v1

import (
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

// StorageClusterSpec defines the desired state of StorageCluster
type StorageClusterSpec struct {
	ManageNodes  bool   `json:"manageNodes,omitempty"`
	InstanceType string `json:"instanceType,omitempty"`
	// External Storage is optional and defaults to false. When set to true, OCS will
	// connect to an external OCS Storage Cluster instead of provisioning one locally.
	ExternalStorage ExternalStorageClusterSpec `json:"externalStorage,omitempty"`
	// HostNetwork defaults to false
	HostNetwork bool `json:"hostNetwork,omitempty"`
	// Resources follows the conventions of and is mapped to CephCluster.Spec.Resources
	Resources          map[string]corev1.ResourceRequirements `json:"resources,omitempty"`
	StorageDeviceSets  []StorageDeviceSet                     `json:"storageDeviceSets,omitempty"`
	MonPVCTemplate     *corev1.PersistentVolumeClaim          `json:"monPVCTemplate,omitempty"`
	MonDataDirHostPath string                                 `json:"monDataDirHostPath,omitempty"`
	// Version specifies the version of StorageCluster
	Version string `json:"version,omitempty"`
}

// ExternalStorageClusterSpec defines the spec of the external Storage Cluster
// to be connected to the local cluster
type ExternalStorageClusterSpec struct {
	// +optional
	Enable bool `json:"enable,omitempty"`
}

// StorageDeviceSet defines a set of storage devices.
// It configures the StorageClassDeviceSets field in Rook-Ceph.
type StorageDeviceSet struct {
	// Count is the number of devices in each StorageClassDeviceSet
	// +kubebuilder:validation:Minimum=1
	Count int `json:"count"`

	// Replica is the number of StorageClassDeviceSets for this
	// StorageDeviceSet
	// +kubebuilder:validation:Minimum=1
	// +optional
	Replica int `json:"replica,omitempty"`

	// TopologyKey is the Kubernetes topology label that the
	// StorageClassDeviceSets will be distributed across. Ignored if
	// Placement is set
	// +optional
	TopologyKey string `json:"topologyKey,omitempty"`

	// Portable says whether the OSDs in this device set can move between
	// nodes. This is ignored if Placement is not set
	// +optional
	Portable bool `json:"portable,omitempty"`

	Name            string                       `json:"name"`
	Resources       corev1.ResourceRequirements  `json:"resources,omitempty"`
	Placement       rook.Placement               `json:"placement,omitempty"`
	Config          StorageDeviceSetConfig       `json:"config,omitempty"`
	DataPVCTemplate corev1.PersistentVolumeClaim `json:"dataPVCTemplate"`
}

// StorageDeviceSetConfig defines Ceph OSD specific config options for the StorageDeviceSet
// TODO: Fill in the members when the actual configurable options are defined in rook-ceph
type StorageDeviceSetConfig struct {
	// TuneSlowDeviceClass tunes the OSD when running on a slow Device Class
	// +optional
	TuneSlowDeviceClass bool `json:"tuneSlowDeviceClass,omitempty"`
}

// StorageClusterStatus defines the observed state of StorageCluster
// +k8s:openapi-gen=true
type StorageClusterStatus struct {
	// Phase describes the Phase of StorageCluster
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the StorageCluster resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this
	// operator. Object references will be added to this list after they have
	// been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`

	// NodeTopologies is a list of topology labels on all nodes matching
	// the StorageCluster's placement selector.
	// +optional
	NodeTopologies *NodeTopologyMap `json:"nodeTopologies,omitempty"`

	// FailureDomain is the base CRUSH element Ceph will use to distribute
	// its data replicas for the default CephBlockPool
	// +optional
	FailureDomain string `json:"failureDomain,omitempty"`

	StorageClassesCreated       bool `json:"storageClassesCreated,omitempty"`
	CephObjectStoresCreated     bool `json:"cephObjectStoresCreated,omitempty"`
	CephBlockPoolsCreated       bool `json:"cephBlockPoolsCreated,omitempty"`
	CephObjectStoreUsersCreated bool `json:"cephObjectStoreUsersCreated,omitempty"`
	CephFilesystemsCreated      bool `json:"cephFilesystemsCreated,omitempty"`
}

// TopologyLabelValues is a list of values for a topology label
type TopologyLabelValues []string

// NodeTopologyMap represents the list of all values of all topology labels
// across all nodes in the StorageCluster
type NodeTopologyMap struct {
	// Labels is a map of topology label keys
	// (e.g. "failure-domain.kubernetes.io") to a set of values for those
	// keys.
	// +optional
	// +nullable
	Labels map[string]TopologyLabelValues `json:"labels,omitempty"`
}

const (
	// ConditionReconcileComplete communicates the status of the StorageCluster resource's
	// reconcile functionality. Basically, is the Reconcile function running to completion.
	ConditionReconcileComplete conditionsv1.ConditionType = "ReconcileComplete"

	// ConditionExternalClusterConnected condition type indicates
	// the successful connection to an external cluster
	ConditionExternalClusterConnected conditionsv1.ConditionType = "ExternalClusterConnected"

	// ConditionExternalClusterConnecting type indicates that rook is still trying for
	// an external connection
	ConditionExternalClusterConnecting conditionsv1.ConditionType = "ExternalClusterConnecting"
)

// List of constants to show different different reconciliation messages and statuses.
const (
	ReconcileFailed                 = "ReconcileFailed"
	ReconcileInit                   = "Init"
	ReconcileCompleted              = "ReconcileCompleted"
	ReconcileCompletedMessage       = "Reconcile completed successfully"
	ExternalClusterConnected        = "ExternalClusterConnected"
	ExternalClusterConnectedMessage = "Connected successfully to an external cluster"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageCluster is the Schema for the storageclusters API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="External",type=boolean,JSONPath=.spec.externalStorage.enable,description="External Storage Cluster"
// +kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Version",type=string,JSONPath=.spec.version,description="Storage Cluster Version"
type StorageCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClusterSpec   `json:"spec,omitempty"`
	Status StorageClusterStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageClusterList contains a list of StorageCluster
type StorageClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageCluster{}, &StorageClusterList{})
}
