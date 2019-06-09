package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageDeviceSetSpec defines the desired state of StorageDeviceSet
type StorageDeviceSetSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
}

// StorageDeviceSetStatus defines the observed state of StorageDeviceSet
type StorageDeviceSetStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageDeviceSet is the Schema for the storagedevicesets API
// +k8s:openapi-gen=true
type StorageDeviceSet struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageDeviceSetSpec   `json:"spec,omitempty"`
	Status StorageDeviceSetStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// StorageDeviceSetList contains a list of StorageDeviceSet
type StorageDeviceSetList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageDeviceSet `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageDeviceSet{}, &StorageDeviceSetList{})
}
