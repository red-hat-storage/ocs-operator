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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

type StorageClusterPeerState string

const (
	// StorageClusterPeerInitializing represents Initializing state of StorageClusterPeerState
	StorageClusterPeerInitializing StorageClusterPeerState = "Initializing"
	// StorageClusterPeerValidating represents Validating state of StorageClusterPeerState
	StorageClusterPeerValidating StorageClusterPeerState = "Validating"
	// StorageClusterPeerFailed represents Failed state of StorageClusterPeerState
	StorageClusterPeerFailed StorageClusterPeerState = "Failed"
	// StorageClusterPeerCreating represents Configuring state of StorageClusterPeerState
	StorageClusterPeerCreating StorageClusterPeerState = "Creating"
	// StorageClusterPeerConfiguring represents Configuring state of StorageClusterPeerState
	StorageClusterPeerConfiguring StorageClusterPeerState = "Configuring"
	// StorageClusterPeerReady represents Ready state of StorageClusterPeerState
	StorageClusterPeerReady StorageClusterPeerState = "Ready"
	// StorageClusterPeerDeleting represents Deleting state of StorageClusterPeerState
	StorageClusterPeerDeleting StorageClusterPeerState = "Deleting"
)

const (
	StorageClusterPeerFinalizer = "storageclusterpeer.ocs.openshift.io"
)

// StorageClusterPeerSpec defines the desired state of StorageClusterPeer
type StorageClusterPeerSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// OCSAPIServerURI is the URI of the secondary ocs api server
	OCSAPIServerURI string `json:"ocsApiServerUri,omitempty"`
}

// StorageClusterPeerStatus defines the observed state of StorageClusterPeer
type StorageClusterPeerStatus struct {

	// Phase describes the Phase of StorageClusterPeer
	// This is used by OLM UI to provide status information to the user
	Phase StorageClusterPeerState `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp
//+kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
//+operator-sdk:csv:customresourcedefinitions:displayName="Storage Cluster Peer"

// StorageClusterPeer is the Schema for the storageclusterpeers API
type StorageClusterPeer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

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
