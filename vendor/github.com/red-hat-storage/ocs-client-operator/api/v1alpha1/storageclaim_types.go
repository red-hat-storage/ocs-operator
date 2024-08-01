/*
Copyright 2022 Red Hat, Inc.

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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type storageClaimState string

const (
	// StorageClaimInitializing represents Initializing state of StorageClaim
	StorageClaimInitializing storageClaimState = "Initializing"
	// StorageClaimValidating represents Validating state of StorageClaim
	StorageClaimValidating storageClaimState = "Validating"
	// StorageClaimFailed represents Failed state of StorageClaim
	StorageClaimFailed storageClaimState = "Failed"
	// StorageClaimCreating represents Configuring state of StorageClaim
	StorageClaimCreating storageClaimState = "Creating"
	// StorageClaimConfiguring represents Configuring state of StorageClaim
	StorageClaimConfiguring storageClaimState = "Configuring"
	// StorageClaimReady represents Ready state of StorageClaim
	StorageClaimReady storageClaimState = "Ready"
	// StorageClaimDeleting represents Deleting state of StorageClaim
	StorageClaimDeleting storageClaimState = "Deleting"
)

// StorageClaimStatus defines the observed state of StorageClaim
type StorageClaimStatus struct {
	Phase storageClaimState `json:"phase,omitempty"`
}

// StorageClaimSpec defines the desired state of StorageClaim
type StorageClaimSpec struct {
	//+kubebuilder:validation:XValidation:rule="self.lowerAscii()=='block'||self.lowerAscii()=='sharedfile'",message="value should be either 'sharedfile' or 'block'"
	Type             string `json:"type"`
	EncryptionMethod string `json:"encryptionMethod,omitempty"`
	StorageProfile   string `json:"storageProfile,omitempty"`
	StorageClient    string `json:"storageClient"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="StorageType",type="string",JSONPath=".spec.type"
//+kubebuilder:printcolumn:name="StorageProfile",type="string",JSONPath=".spec.storageProfile"
//+kubebuilder:printcolumn:name="StorageClientName",type="string",JSONPath=".spec.storageClient"
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"

// StorageClaim is the Schema for the storageclaims API
type StorageClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:XValidation:rule="oldSelf == self",message="spec is immutable"
	Spec   StorageClaimSpec   `json:"spec,omitempty"`
	Status StorageClaimStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageClaimList contains a list of StorageClaim
type StorageClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageClaim{}, &StorageClaimList{})
}
