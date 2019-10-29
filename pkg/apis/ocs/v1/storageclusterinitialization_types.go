package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageClusterInitializationSpec defines the desired state of StorageClusterInitialization
// +k8s:openapi-gen=true
type StorageClusterInitializationSpec struct {
}

// StorageClusterInitializationStatus defines the observed state of StorageClusterInitialization
// +k8s:openapi-gen=true
type StorageClusterInitializationStatus struct {
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageClusterInitialization is the Schema for the storageclusterinitializations API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type StorageClusterInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClusterInitializationSpec   `json:"spec,omitempty"`
	Status StorageClusterInitializationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageClusterInitializationList contains a list of StorageClusterInitialization
type StorageClusterInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClusterInitialization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageClusterInitialization{}, &StorageClusterInitializationList{})
}
