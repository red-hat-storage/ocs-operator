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
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type StorageClusterPeerState string
type StorageClusterPeerFailureReason string

const (
	StorageClusterPeerStateInitializing StorageClusterPeerState = "Initializing"
	StorageClusterPeerStatePending      StorageClusterPeerState = "Pending"
	StorageClusterPeerStatePeered       StorageClusterPeerState = "Peered"
	StorageClusterPeerStateFailed       StorageClusterPeerState = "Failed"

	StorageClusterPeerFailureReasonClusterNotFound         StorageClusterPeerFailureReason = "CLUSTER_NOT_FOUND"
	StorageClusterPeerFailureReasonInvalidOnboardingTicket StorageClusterPeerFailureReason = "INVALID_ONBOARDING_TICKET"
	StorageClusterPeerFailureReasonConnectionFailed        StorageClusterPeerFailureReason = "CONNECTION_FAILED"
)

type PeerInfo struct {
	StorageClusterUid string `json:"storageClusterUid,omitempty"`
}

// StorageClusterPeerSpec defines the desired state of StorageClusterPeer
type StorageClusterPeerSpec struct {

	// ApiEndpoint is the URI of the ODF api server
	ApiEndpoint string `json:"apiEndpoint"`

	// OnboardingToken holds an identity information required by the local ODF cluster to onboard.
	OnboardingToken string `json:"onboardingToken"`

	// ServerCASecret references a secret containing the peer server CA certificate.
	// +optional
	ServerCASecret *corev1.LocalObjectReference `json:"serverCASecret,omitempty"`

	// ServerName is the expected server identity (SAN) in the peer server certificate.
	// +optional
	ServerName string `json:"serverName,omitempty"`

	// ClientCertSecret references a secret containing the client certificate.
	// +optional
	ClientCertSecret *corev1.LocalObjectReference `json:"clientCertSecret,omitempty"`

	// ClientCASecret references a secret containing the CA certificate used to validate incoming client certificates.
	// +optional
	ClientCASecret *corev1.LocalObjectReference `json:"clientCASecret,omitempty"`

	// ClientSAN is the expected client identity (SAN) in the peer client certificate.
	// +optional
	ClientSAN string `json:"clientSAN,omitempty"`
}

// StorageClusterPeerStatus defines the observed state of StorageClusterPeer
type StorageClusterPeerStatus struct {
	State         StorageClusterPeerState         `json:"state,omitempty"`
	FailureReason StorageClusterPeerFailureReason `json:"failureReason,omitempty"`
	PeerInfo      *PeerInfo                       `json:"peerInfo"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// StorageClusterPeer is the Schema for the storageclusterpeers API
type StorageClusterPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	Spec   StorageClusterPeerSpec   `json:"spec,omitempty"`
	Status StorageClusterPeerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageClusterPeerList contains a list of StorageClusterPeer
type StorageClusterPeerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClusterPeer `json:"items"`
}
