/*
Copyright 2020 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
)

// OCSInitializationSpec defines the desired state of OCSInitialization
type OCSInitializationSpec struct {
	// EnableCephTools toggles on whether or not the ceph tools pod
	// should be deployed.
	// Defaults to false
	// +optional
	// +kubebuilder:deprecatedversion:warning="This field doesn't work anymore and will be removed in future. The ceph tool box can be enabled by setting the spec.enableCephTools field in the storage cluster CR"
	EnableCephTools bool `json:"enableCephTools,omitempty"`

	// Tolerations if specified set toolbox ceph tools pod tolerations
	// Defaults to empty
	// +optional
	// +kubebuilder:deprecatedversion:warning="This field doesn't work anymore and will be removed in future. The tolerations along with any other placement spec are now set by adding them in the storage cluster CR under spec.placement[toolbox]"
	Tolerations []corev1.Toleration `json:"tolerations,omitempty"`
}

// OCSInitializationStatus defines the observed state of OCSInitialization
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
	RelatedObjects                []corev1.ObjectReference     `json:"relatedObjects,omitempty"`
	ErrorMessage                  string                       `json:"errorMessage,omitempty"`
	SCCsCreated                   bool                         `json:"sCCsCreated,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="Created At",type=string,JSONPath=.metadata.creationTimestamp
// +operator-sdk:csv:customresourcedefinitions:displayName="OCS Initialization"

// OCSInitialization represents the initial data to be created when the operator is installed.
type OCSInitialization struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   OCSInitializationSpec   `json:"spec,omitempty"`
	Status OCSInitializationStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// OCSInitializationList contains a list of OCSInitialization
type OCSInitializationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []OCSInitialization `json:"items"`
}
