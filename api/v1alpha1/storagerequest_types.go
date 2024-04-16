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

// StorageClassRequestSpec defines the desired state of StorageClassRequest
type StorageClassRequestSpec struct {
	//+kubebuilder:validation:Enum=blockpool;sharedfilesystem
	Type             string `json:"type"`
	EncryptionMethod string `json:"encryptionMethod,omitempty"`
	StorageProfile   string `json:"storageProfile,omitempty"`
}

type StorageClassRequestState string

const (
	// StorageClassRequestInitializing represents Initializing state of StorageClassRequest
	StorageClassRequestInitializing StorageClassRequestState = "Initializing"
	// StorageClassRequestValidating represents Validating state of StorageClassRequest
	StorageClassRequestValidating StorageClassRequestState = "Validating"
	// StorageClassRequestFailed represents Failed state of StorageClassRequest
	StorageClassRequestFailed StorageClassRequestState = "Failed"
	// StorageClassRequestCreating represents Configuring state of StorageClassRequest
	StorageClassRequestCreating StorageClassRequestState = "Creating"
	// StorageClassRequestConfiguring represents Configuring state of StorageClassRequest
	StorageClassRequestConfiguring StorageClassRequestState = "Configuring"
	// StorageClassRequestReady represents Ready state of StorageClassRequest
	StorageClassRequestReady StorageClassRequestState = "Ready"
	// StorageClassRequestDeleting represents Deleting state of StorageClassRequest
	StorageClassRequestDeleting StorageClassRequestState = "Deleting"
)

const (
	StorageClassRequestFinalizer  = "StorageClassRequest.ocs.openshift.io"
	StorageClassRequestAnnotation = "ocs.openshift.io.storageclassrequest"
	CephFileSystemDataPoolLabel   = "cephfilesystem.datapool.name"
)

// StorageClassRequestStatus defines the observed state of StorageClassRequest
type StorageClassRequestStatus struct {
	Phase StorageClassRequestState `json:"phase,omitempty"`
	// CephResources provide details of created ceph resources required for external storage
	CephResources []*CephResourcesSpec `json:"cephResources,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="StorageType",type="string",JSONPath=".spec.type"
// +kubebuilder:printcolumn:name="Phase",type="string",JSONPath=".status.phase"

// StorageClassRequest is the Schema for the StorageClassRequests API
type StorageClassRequest struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   StorageClassRequestSpec   `json:"spec"`
	Status StorageClassRequestStatus `json:"status,omitempty"`
}

// +kubebuilder:object:root=true

// StorageClassRequestList contains a list of StorageClassRequest
type StorageClassRequestList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageClassRequest `json:"items"`
}
