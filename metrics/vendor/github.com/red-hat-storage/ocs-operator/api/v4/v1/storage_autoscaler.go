/*
Copyright 2025 Red Hat OpenShift Container Storage.

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
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageAutoScalerSpec defines the desired state of StorageAutoScaler
type StorageAutoScalerSpec struct {
	// StorageClusterName is the name of the storage cluster for which the storage scaling is to be done.
	// +kubebuilder:validation:Required
	StorageClusterName string `json:"storageClusterName,omitempty"`
	// DeviceClassName is the name of the device class for which the storage scaling is to be done.
	// +kubebuilder:validation:Required
	DeviceClassName string `json:"deviceClassName,omitempty"`
	// CapacityLimit is the capacity limit for the storage scaling for the specific deviceClass and storagecluster.
	// +kubebuilder:validation:Required
	CapacityLimit resource.Quantity `json:"capacityLimit,omitempty"`
}

type StorageAutoScalerPhase string

const (
	PhaseWaiting    StorageAutoScalerPhase = "Waiting"
	PhaseInProgress StorageAutoScalerPhase = "InProgress"
	PhaseFailed     StorageAutoScalerPhase = "Failed"
	PhaseCompleted  StorageAutoScalerPhase = "Completed"
)

// StorageAutoScalerStatus defines the observed state of StorageAutoScaler
type StorageAutoScalerStatus struct {
	// Phase describes the Phase of StorageAutoScaler
	// +kubebuilder:validation:Enum=Waiting;InProgress;Failed;Completed
	Phase StorageAutoScalerPhase `json:"phase,omitempty"`
	// LastRunTimeStamp is the time stamp of the last run of the storage scaling
	LastRunTimeStamp metav1.Time `json:"lastRunTimeStamp,omitempty"`
	// The current size is the original size of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD size.
	// Used for vertical scaling of OSDs.
	CurrentOsdSize resource.Quantity `json:"currentOsdSize,omitempty"`
	// The ExpectedOsdSize is the size that the auto-expansion has decided to set.
	// This will be set on the storageCLuster CR as the desired size of the OSDs.
	// Used for vertical scaling of OSDs.
	ExpectedOsdSize resource.Quantity `json:"expectedOsdSize,omitempty"`
	// The current OSD count is the original count of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD count.
	// Used for horizontal scaling of OSDs.
	CurrentOsdCount uint16 `json:"currentOsdCount,omitempty"`
	// The Expected OSD count is the count that the auto-expansion has decided to set.
	// This will be set on the storageCluster CR as the desired count of the OSDs.
	// Used for horizontal scaling of OSDs.
	ExpectedOsdCount uint16 `json:"expectedOsdCount,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp, description="Age"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="LastRunTimeStamp",type=date,JSONPath=.status.lastRunTimeStamp,description="Last Run Time Stamp"
// +operator-sdk:csv:customresourcedefinitions:displayName="Auto Storage Scaling"

// StorageAutoScaler represents the automatic storage scaling for storage cluster.
type StorageAutoScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageAutoScalerSpec   `json:"spec,omitempty"`
	Status StorageAutoScalerStatus `json:"status,omitempty"`
}
