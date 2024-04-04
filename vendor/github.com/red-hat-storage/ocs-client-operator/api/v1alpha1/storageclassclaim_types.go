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

type storageClassClaimState string

const (
	// StorageClassClaimInitializing represents Initializing state of StorageClassClaim
	StorageClassClaimInitializing storageClassClaimState = "Initializing"
	// StorageClassClaimValidating represents Validating state of StorageClassClaim
	StorageClassClaimValidating storageClassClaimState = "Validating"
	// StorageClassClaimFailed represents Failed state of StorageClassClaim
	StorageClassClaimFailed storageClassClaimState = "Failed"
	// StorageClassClaimCreating represents Configuring state of StorageClassClaim
	StorageClassClaimCreating storageClassClaimState = "Creating"
	// StorageClassClaimConfiguring represents Configuring state of StorageClassClaim
	StorageClassClaimConfiguring storageClassClaimState = "Configuring"
	// StorageClassClaimReady represents Ready state of StorageClassClaim
	StorageClassClaimReady storageClassClaimState = "Ready"
	// StorageClassClaimDeleting represents Deleting state of StorageClassClaim
	StorageClassClaimDeleting storageClassClaimState = "Deleting"
)

// StorageClassClaimStatus defines the observed state of StorageClassClaim
type StorageClassClaimStatus struct {
	Phase       storageClassClaimState `json:"phase,omitempty"`
	SecretNames []string               `json:"secretNames,omitempty"`
}

type StorageClientNamespacedName struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// StorageClassClaimSpec defines the desired state of StorageClassClaim
type StorageClassClaimSpec struct {
	//+kubebuilder:validation:Enum=blockpool;sharedfilesystem
	Type             string                       `json:"type"`
	EncryptionMethod string                       `json:"encryptionMethod,omitempty"`
	StorageProfile   string                       `json:"storageProfile,omitempty"`
	StorageClient    *StorageClientNamespacedName `json:"storageClient"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="StorageType",type="string",JSONPath=".spec.type"
//+kubebuilder:printcolumn:name="StorageProfile",type="string",JSONPath=".spec.storageProfile"
//+kubebuilder:printcolumn:name="StorageClientName",type="string",JSONPath=".spec.storageClient.name"
//+kubebuilder:printcolumn:name="StorageClientNamespace",type="string",JSONPath=".spec.storageClient.namespace"
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"

// StorageClassClaim is the Schema for the storageclassclaims API
type StorageClassClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:XValidation:rule="oldSelf == self",message="spec is immutable"
	Spec   StorageClassClaimSpec   `json:"spec"`
	Status StorageClassClaimStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageClassClaimList contains a list of StorageClassClaim
type StorageClassClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClassClaim `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageClassClaim{}, &StorageClassClaimList{})
}
