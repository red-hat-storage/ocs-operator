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
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// StorageAutoScalerSpec defines the desired state of StorageAutoScaler
type StorageAutoScalerSpec struct {
	// StorageCluster is the name of the storage cluster for which the storage scaling is to be done.
	// +kubebuilder:validation:Required
	StorageCluster corev1.LocalObjectReference `json:"storageCluster,omitempty"`
	// DeviceClass is the name of the device class for which the storage scaling is to be done.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="ssd"
	DeviceClass string `json:"deviceClass,omitempty"`
	// StorageCapacityLimit is the total aggregate capacity limit for the storage scaling for the specific deviceClass and storagecluster.
	// +kubebuilder:validation:Required
	StorageCapacityLimit resource.Quantity `json:"storageCapacityLimit,omitempty"`
	// MaxOsdSize is the maximum size that Osd disk can be expanded to.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="8Ti"
	MaxOsdSize resource.Quantity `json:"maxOsdSize,omitempty"`
	// StorageScalingThresholdPercent is the threshold percentage of the storage capacity that triggers the auto-scaling of the OSDs.
	// Should be less than the OsdNearFullThresholdPercentage.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=70
	StorageScalingThresholdPercent int `json:"storageScalingThresholdPercent,omitempty"`
	// TimeoutSeconds is the time in seconds after which the storage auto-scaler will alert the user that the scaling operation has been failed.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1800
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type StorageAutoScalerPhase string

const (
	StorageAutoScalerPhaseInvalid    StorageAutoScalerPhase = "Invalid"
	StorageAutoScalerPhaseNotStarted StorageAutoScalerPhase = "NotStarted"
	StorageAutoScalerPhaseInProgress StorageAutoScalerPhase = "InProgress"
	StorageAutoScalerPhaseSucceeded  StorageAutoScalerPhase = "Succeeded"
	StorageAutoScalerPhaseFailed     StorageAutoScalerPhase = "Failed"
)

// TimestampedError is the error message with the time stamp.
type TimestampedError struct {
	// Message is the error message in case the storage scaling operation has failed.
	Message string `json:"message,omitempty"`
	// Timestamp is the time stamp when the error occurred.
	Timestamp metav1.Time `json:"timestamp,omitempty"`
}

type LastExpansionStatus struct {
	// StartTime is the time stamp of the last run start of the storage scaling
	// +optional
	StartTime metav1.Time `json:"startTime,omitempty"`
	// CompletionTime is the time stamp of the last run completion of the storage scaling
	// +optional
	CompletionTime metav1.Time `json:"completionTime,omitempty"`
	// The start OSD size is the original size of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD size.
	// Used for vertical scaling of OSDs.
	// +optional
	StartOsdSize resource.Quantity `json:"startOsdSize,omitempty"`
	// The ExpectedOsdSize is the size that the auto-expansion has decided to set.
	// This will be set on the storageCLuster CR as the desired size of the OSDs.
	// Used for vertical scaling of OSDs.
	// +optional
	ExpectedOsdSize resource.Quantity `json:"expectedOsdSize,omitempty"`
	// The start OSD count is the original count of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD count.
	// Used for horizontal scaling of OSDs.
	// +optional
	StartOsdCount uint16 `json:"startOsdCount,omitempty"`
	// The Expected OSD count is the count that the auto-expansion has decided to set.
	// This will be set on the storageCluster CR as the desired count of the OSDs.
	// Used for horizontal scaling of OSDs.
	// +optional
	ExpectedOsdCount uint16 `json:"expectedOsdCount,omitempty"`
	// StartStorageCapacity is the original storage capacity of the storage cluster before the expansion in progress is completed.
	StartStorageCapacity resource.Quantity `json:"startStorageCapacity,omitempty"`
	// ExpectedStorageCapacity is the expected storage capacity of the storage cluster after the expansion in progress is completed.
	ExpectedStorageCapacity resource.Quantity `json:"expectedStorageCapacity,omitempty"`
}

// StorageAutoScalerStatus defines the observed state of StorageAutoScaler
type StorageAutoScalerStatus struct {
	// Phase describes the Phase of StorageAutoScaler
	// +optional
	Phase StorageAutoScalerPhase `json:"phase,omitempty"`
	// Error is the error message in case the storage scaling operation has failed.
	// +optional
	// +nullable
	Error *TimestampedError `json:"error,omitempty"`
	// +optional
	// +nullable
	LastExpansion *LastExpansionStatus `json:"lastExpansion,omitempty"`
	// StorageCapacityLimitReached is the flag that indicates if the storage capacity limit has been reached.
	// +optional
	// +nullable
	StorageCapacityLimitReached *bool `json:"storageCapacityLimitReached,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Age",type=date,JSONPath=.metadata.creationTimestamp, description="Age"
// +kubebuilder:printcolumn:name="Phase",type=string,JSONPath=.status.phase,description="Current Phase"
// +kubebuilder:printcolumn:name="DeviceClass",type=string,JSONPath=.spec.deviceClass,description="Device Class"
// +kubebuilder:printcolumn:name="LastRunStartTime",type=date,JSONPath=.status.lastExpansion.startTime,description="Last Run Start Time Stamp"
// +kubebuilder:printcolumn:name="LastRunCompletionTime",type=date,JSONPath=.status.lastExpansion.completionTime,description="Last Run Completion Time Stamp"
// +operator-sdk:csv:customresourcedefinitions:displayName="Storage Auto Scaling"

// StorageAutoScaler represents the automatic storage scaling for storage cluster.
type StorageAutoScaler struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageAutoScalerSpec   `json:"spec,omitempty"`
	Status StorageAutoScalerStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageAutoScalerList contains a list of StorageAutoScaler
type StorageAutoScalerList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageAutoScaler `json:"items"`
}
