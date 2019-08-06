package v1alpha1

import (
	rookalpha "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageClusterSpec defines the desired state of StorageCluster
type StorageClusterSpec struct {
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
	ManageNodes       bool               `json:"manageNodes,omitempty"`
	InstanceType      string             `json:"instanceType,omitempty"`
	StorageDeviceSets []StorageDeviceSet `json:"storageDeviceSets,omitempty"`
}

// StorageDeviceSet defines a set of storage devices.
// It is derived from and mapped to StorageClassDeviceSet in Rook.
type StorageDeviceSet struct {
	Name            string                       `json:"name"`
	Count           int                          `json:"count"`
	Resources       corev1.ResourceRequirements  `json:"resources"`
	Placement       rookalpha.Placement          `json:"placement"`
	Config          StorageDeviceSetConfig       `json:"config,omitempty"`
	DataPVCTemplate corev1.PersistentVolumeClaim `json:"dataPVCTemplate"`
}

// StorageDeviceSetConfig defines Ceph OSD specific config options for the StorageDeviceSet
// TODO: Fill in the members when the actual configurable options are defined in rook-ceph
type StorageDeviceSetConfig struct{}

// StorageClusterStatus defines the observed state of StorageCluster
type StorageClusterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageCluster is the Schema for the storageclusters API
// +k8s:openapi-gen=true
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
