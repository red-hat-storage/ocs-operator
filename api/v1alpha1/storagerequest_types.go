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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageRequestSpec defines the desired state of StorageRequest
type StorageRequestSpec struct {
	//+kubebuilder:validation:Enum=block;sharedfile
	Type             string `json:"type"`
	EncryptionMethod string `json:"encryptionMethod,omitempty"`
	StorageProfile   string `json:"storageProfile,omitempty"`
}

type StorageRequestState string

const (
	// StorageRequestInitializing represents Initializing state of StorageRequest
	StorageRequestInitializing StorageRequestState = "Initializing"
	// StorageRequestValidating represents Validating state of StorageRequest
	StorageRequestValidating StorageRequestState = "Validating"
	// StorageRequestFailed represents Failed state of StorageRequest
	StorageRequestFailed StorageRequestState = "Failed"
	// StorageRequestCreating represents Configuring state of StorageRequest
	StorageRequestCreating StorageRequestState = "Creating"
	// StorageRequestConfiguring represents Configuring state of StorageRequest
	StorageRequestConfiguring StorageRequestState = "Configuring"
	// StorageRequestReady represents Ready state of StorageRequest
	StorageRequestReady StorageRequestState = "Ready"
	// StorageRequestDeleting represents Deleting state of StorageRequest
	StorageRequestDeleting StorageRequestState = "Deleting"
)

const (
	StorageRequestFinalizer     = "StorageRequest.ocs.openshift.io"
	StorageRequestAnnotation    = "ocs.openshift.io.storagerequest"
	CephFileSystemDataPoolLabel = "cephfilesystem.datapool.name"
)

// StorageRequestStatus defines the observed state of StorageRequest
type StorageRequestStatus struct {
	Phase StorageRequestState `json:"phase,omitempty"`
	// CephResources provide details of created ceph resources required for external storage
	CephResources []*CephResourcesSpec `json:"cephResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="StorageType",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"

// StorageRequest is the Schema for the StorageRequests API
type StorageRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageRequestSpec   `json:"spec"`
	Status StorageRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StorageRequestList contains a list of StorageRequest
type StorageRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageRequest `json:"items"`
}
