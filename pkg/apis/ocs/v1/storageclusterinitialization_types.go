package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
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
	// Phase describes the Phase of StorageClusterInitialization
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the StorageClusterInitialization resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this
	// operator. Object references will be added to this list after they have
	// been created AND found in the cluster.
	// +optional
	RelatedObjects              []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	StorageClassesCreated       bool                     `json:"storageClassesCreated,omitempty"`
	CephObjectStoresCreated     bool                     `json:"cephObjectStoresCreated,omitempty"`
	CephBlockPoolsCreated       bool                     `json:"cephBlockPoolsCreated,omitempty"`
	CephObjectStoreUsersCreated bool                     `json:"cephObjectStoreUsersCreated,omitempty"`
	CephFilesystemsCreated      bool                     `json:"cephFilesystemsCreated,omitempty"`
	NoobaaSystemCreated         bool                     `json:"noobaaSystemCreated,omitempty"`
	NoobaaServiceMonitorCreated bool                     `json:"noobaaServiceMonitorCreated,omitempty"`
	ErrorMessage                string                   `json:"errorMessage,omitempty"`
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
