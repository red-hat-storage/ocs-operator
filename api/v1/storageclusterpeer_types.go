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

	// ServerCAConfigMap selects the key in a configmap holding the PEM-encoded peer
	// server CA certificate. Allows pointing at an existing CA bundle (e.g. an
	// OpenShift-managed configmap) whose key is not "ca.crt".
	// +optional
	ServerCAConfigMap corev1.ConfigMapKeySelector `json:"serverCAConfigMap,omitempty"`

	// ServerSAN specifies the expected DNS Subject Alternative Name (SAN) in the server certificate.
	// +optional
	ServerSAN string `json:"serverSAN,omitempty"`

	// ClientCertSecret references a secret containing the client certificate.
	// The secret must be of type kubernetes.io/tls with keys "tls.crt" and "tls.key".
	// +optional
	ClientCertSecret corev1.LocalObjectReference `json:"clientCertSecret,omitempty"`

	// ClientCAConfigMap references a configmap containing the CA certificate used to validate client certificates.
	// The configmap must contain a key named "ca.crt" with the PEM-encoded CA certificate.
	// +optional
	ClientCAConfigMap corev1.LocalObjectReference `json:"clientCAConfigMap,omitempty"`

	// ClientSAN specifies the expected DNS Subject Alternative Name (SAN) in the client certificate.
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
