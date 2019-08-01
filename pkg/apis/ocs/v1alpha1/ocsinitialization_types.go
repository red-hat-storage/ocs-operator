package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// OCSInitializationSpec defines the desired state of OCSInitialization
// +k8s:openapi-gen=true
type OCSInitializationSpec struct {
}

// OCSInitializationStatus defines the observed state of OCSInitialization
// +k8s:openapi-gen=true
type OCSInitializationStatus struct {
	StorageClassesCreated bool   `json:"storageClassesCreated,omitempty"`
	ErrorMessage          string `json:"errorMessage,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OCSInitialization is the Schema for the ocsinitialization API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
type OCSInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCSInitializationSpec   `json:"spec,omitempty"`
	Status OCSInitializationStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OCSInitializationList contains a list of OCSInitialization
type OCSInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCSInitialization `json:"items"`
}

func init() {
	SchemeBuilder.Register(&OCSInitialization{}, &OCSInitializationList{})
}
