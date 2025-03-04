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

type storageClientPhase string

const (
	// StorageClientInitializing represents Initializing state of storageClient
	StorageClientInitializing storageClientPhase = "Initializing"
	// StorageClientOnboarding represents Onboarding state of storageClient
	StorageClientOnboarding storageClientPhase = "Onboarding"
	// StorageClientOnboardingProgressing represents OnboardingProgressing state of storageClient
	StorageClientOnboardingProgressing storageClientPhase = "Progressing"
	// StorageClientConnected represents Onboarding state of storageClient
	StorageClientConnected storageClientPhase = "Connected"
	// StorageClientOffboarding represents Onboarding state of storageClient
	StorageClientOffboarding storageClientPhase = "Offboarding"
	// StorageClientFailed represents Failed state of storageClient
	StorageClientFailed storageClientPhase = "Failed"
)

// StorageClientSpec defines the desired state of StorageClient
type StorageClientSpec struct {
	// StorageProviderEndpoint holds info to establish connection with the storage providing cluster.
	StorageProviderEndpoint string `json:"storageProviderEndpoint"`

	// OnboardingTicket holds an identity information required for consumer to onboard.
	OnboardingTicket string `json:"onboardingTicket"`
}

// StorageClientStatus defines the observed state of StorageClient
type StorageClientStatus struct {
	Phase storageClientPhase `json:"phase,omitempty"`

	InMaintenanceMode bool `json:"inMaintenanceMode,omitempty"`

	// ConsumerID will hold the identity of this cluster inside the attached provider cluster
	ConsumerID string `json:"id,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster
//+kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"
//+kubebuilder:printcolumn:name="consumer",type="string",JSONPath=".status.id"

// StorageClient is the Schema for the storageclients API
type StorageClient struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClientSpec   `json:"spec,omitempty"`
	Status StorageClientStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageClientList contains a list of StorageClient
type StorageClientList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClient `json:"items"`
}

func init() {
	SchemeBuilder.Register(&StorageClient{}, &StorageClientList{})
}
