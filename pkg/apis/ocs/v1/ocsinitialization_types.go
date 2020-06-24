package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.
// Important: Run "operator-sdk generate k8s" to regenerate code after modifying this file

// OCSInitializationSpec defines the desired state of OCSInitialization
// +k8s:openapi-gen=true
type OCSInitializationSpec struct {
	// EnableCephTools toggles on whether or not the ceph tools pod
	// should be deployed.
	// Defaults to false
	// +optional
	EnableCephTools bool `json:"enableCephTools,omitempty"`
}

// OCSInitializationStatus defines the observed state of OCSInitialization
// +k8s:openapi-gen=true
type OCSInitializationStatus struct {
	// Phase describes the Phase of OCSInitialization
	// This is used by OLM UI to provide status information
	// to the user
	Phase string `json:"phase,omitempty"`

	// Conditions describes the state of the OCSInitialization resource.
	// +optional
	Conditions []conditionsv1.Condition `json:"conditions,omitempty"`

	// RelatedObjects is a list of objects created and maintained by this
	// operator. Object references will be added to this list after they have
	// been created AND found in the cluster.
	// +optional
	RelatedObjects []corev1.ObjectReference `json:"relatedObjects,omitempty"`
	ErrorMessage   string                   `json:"errorMessage,omitempty"`
	SCCsCreated    bool                     `json:"sCCsCreated,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// OCSInitialization is the Schema for the ocsinitialization API
// +k8s:openapi-gen=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
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
