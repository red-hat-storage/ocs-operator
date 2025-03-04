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
	// StorageClusterName is the name of the storage cluster for which the storage scaling is to be done.
	// +kubebuilder:validation:Required
	StorageCluster corev1.LocalObjectReference `json:"storageCluster,omitempty"`
	// DeviceClassName is the name of the device class for which the storage scaling is to be done.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="ssd"
	DeviceClassName string `json:"deviceClassName,omitempty"`
	// DeviceClassStorageCapacityLimit is the capacity limit for the storage scaling for the specific deviceClass and storagecluster.
	// +kubebuilder:validation:Required
	DeviceClassStorageCapacityLimit resource.Quantity `json:"deviceClassStorageCapacityLimit,omitempty"`
	// MaxOSDSize is the maximum size that OSD disk can be expanded to.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default="8Ti"
	MaxOSDSize resource.Quantity `json:"maxOSDSize,omitempty"`
	// OsdScalingThresholdPercent is the threshold percentage of the storage capacity that triggers the auto-scaling of the OSDs.
	// Should be less than the OsdNearFullThresholdPercentage.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=70
	OsdScalingThresholdPercent int `json:"osdScalingThresholdPercent,omitempty"`
	// TimeoutSeconds is the time in seconds after which the storage auto-scaler will alert the user that the scaling operation has been failed.
	// +kubebuilder:validation:Optional
	// +kubebuilder:default=1800
	TimeoutSeconds int `json:"timeoutSeconds,omitempty"`
}

type StorageAutoScalerPhase string

const (
	StorageAutoScalerPhaseNotStarted StorageAutoScalerPhase = "NotStarted"
	StorageAutoScalerPhaseInProgress StorageAutoScalerPhase = "InProgress"
	StorageAutoScalerPhaseSucceeded  StorageAutoScalerPhase = "Succeeded"
)

// StorageAutoScalerStatus defines the observed state of StorageAutoScaler
type StorageAutoScalerStatus struct {
	// Phase describes the Phase of StorageAutoScaler
	// +kubebuilder:validation:Enum=NotStarted;InProgress;Failed;Succeeded
	Phase StorageAutoScalerPhase `json:"phase,omitempty"`
	// LastExpansionStartTime is the time stamp of the last run start of the storage scaling
	LastExpansionStartTime metav1.Time `json:"lastExpansionStartTime,omitempty"`
	// LastExpansionCompletionTime is the time stamp of the last run completion of the storage scaling
	LastExpansionCompletionTime metav1.Time `json:"lastExpansionCompletionTime,omitempty"`
	// The start OSD size is the original size of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD size.
	// Used for vertical scaling of OSDs.
	StartOsdSize resource.Quantity `json:"startOsdSize,omitempty"`
	// The ExpectedOsdSize is the size that the auto-expansion has decided to set.
	// This will be set on the storageCLuster CR as the desired size of the OSDs.
	// Used for vertical scaling of OSDs.
	ExpectedOsdSize resource.Quantity `json:"expectedOsdSize,omitempty"`
	// The start OSD count is the original count of the OSDs before the expansion in progress is completed.
	// After the expansion is completed, this would be updated to the expected OSD count.
	// Used for horizontal scaling of OSDs.
	StartOsdCount uint16 `json:"startOsdCount,omitempty"`
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
