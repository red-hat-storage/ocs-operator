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
type NamespacedName struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
}

// RemoteClusterSpec specifies the spec required for the remote cluster
type RemoteClusterSpec struct {
	// ApiEndpoint is the URI of the ODF api server
	ApiEndpoint string `json:"apiEndpoint"`

	// OnboardingTicket holds an identity information required by the local ODF cluster to onboard.
	OnboardingTicket string `json:"onboardingTicket"`

	// StorageClusterName holds the namespacedName of the Remote ODF Cluster
	StorageClusterName NamespacedName `json:"storageClusterName"`
}

// LocalClusterSpec specifies the spec required for the local cluster
type LocalClusterSpec struct {
	// Name holds the name of the local ODF cluster
	Name corev1.LocalObjectReference `json:"name"`
}

// BlockPoolMirroringSpec enables setting up of mirroring for blockPools in the same namespace.
type BlockPoolMirroringSpec struct {
	// Selector is used to select blockPools by label
	Selector metav1.LabelSelector `json:"selector"`
}

// StorageClusterPeerSpec defines the desired state of StorageClusterPeer
type StorageClusterPeerSpec struct {

	// RemoteCluster specifies the spec required for the remote cluster
	RemoteCluster RemoteClusterSpec `json:"remoteCluster"`

	// LocalCluster specifies the spec required for the local cluster
	LocalCluster LocalClusterSpec `json:"localCluster"`

	// BlockPoolMirroring indicates ceph block mirroring between block pool on the local and remote clusters
	//+optional
	BlockPoolMirroring *BlockPoolMirroringSpec `json:"blockPoolMirroring,omitempty"`
}

// StorageClusterPeerStatus defines the observed state of StorageClusterPeer
type StorageClusterPeerStatus struct {
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
