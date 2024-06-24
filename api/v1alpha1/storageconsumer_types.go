/*
Copyright 2021 Red Hat OpenShift Container Storage.

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

// StorageConsumerState represent a StorageConsumer's state
type StorageConsumerState string

const (
	// StorageConsumerStateReady represents Ready state of StorageConsumer
	StorageConsumerStateReady StorageConsumerState = "Ready"
	// StorageConsumerStateConfiguring represents Configuring state of StorageConsumer
	StorageConsumerStateConfiguring StorageConsumerState = "Configuring"
	// StorageConsumerStateDeleting represents Deleting state of StorageConsumer
	StorageConsumerStateDeleting StorageConsumerState = "Deleting"
	// StorageConsumerStateFailed represents Failed state of StorageConsumer
	StorageConsumerStateFailed StorageConsumerState = "Failed"
	// StorageConsumerStateDisabled represents Disabled state of StorageConsumer
	StorageConsumerStateDisabled StorageConsumerState = "Disabled"
)

// StorageConsumerSpec defines the desired state of StorageConsumer
type StorageConsumerSpec struct {
	// Enable flag ignores a reconcile if set to false
	Enable bool `json:"enable,omitempty"`
}

// CephResourcesSpec hold details of created ceph resources required for external storage
type CephResourcesSpec struct {
	// Kind describes the kind of created ceph resource
	Kind string `json:"kind,omitempty"`
	// Name describes the name of created ceph resource
	Name string `json:"name,omitempty"`
	// Phase describes the phase of created ceph resource
	Phase string `json:"status,omitempty"`
	// CephClients holds the name of CephClients mapped to the created ceph resource
	CephClients map[string]string `json:"cephClients,omitempty"`
}

// StorageConsumerStatus defines the observed state of StorageConsumer
type StorageConsumerStatus struct {
	// State describes the state of StorageConsumer
	State StorageConsumerState `json:"state,omitempty"`
	// CephResources provide details of created ceph resources required for external storage
	CephResources []*CephResourcesSpec `json:"cephResources,omitempty"`
	// Timestamp of last heartbeat received from consumer
	LastHeartbeat metav1.Time `json:"lastHeartbeat,omitempty"`
	// Information of storage client received from consumer
	Client ClientStatus `json:"client,omitempty"`
}

// ClientStatus is the information pushed from connected storage client
type ClientStatus struct {
	// StorageClient Platform Version
	// +optional
	PlatformVersion string `json:"platformVersion,omitempty"`

	// StorageClient Operator Version
	// +optional
	OperatorVersion string `json:"operatorVersion,omitempty"`

	// ClusterID is the id of the openshift cluster
	// +optional
	ClusterID string `json:"clusterId,omitempty"`

	// ClusterName is the name of the openshift cluster
	// +optional
	ClusterName string `json:"clusterName,omitempty"`

	// Name is the name of connected storageclient
	// +optional
	Name string `json:"name,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// StorageConsumer is the Schema for the storageconsumers API
type StorageConsumer struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageConsumerSpec   `json:"spec,omitempty"`
	Status StorageConsumerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageConsumerList contains a list of StorageConsumer
type StorageConsumerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageConsumer `json:"items"`
}
