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
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!
// NOTE: json tags are required.  Any new fields you add must have json tags for the fields to be serialized.

// StorageProfileSpec defines the desired state of StorageProfile
type StorageProfileSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:MaxLength=512
	// DeviceClass is the deviceclass name.
	DeviceClass string `json:"deviceClass"`

	// +kubebuilder:validation:Optional
	// configurations to use for cephfilesystem.
	SharedFilesystemConfiguration SharedFilesystemConfigurationSpec `json:"sharedFilesystemConfiguration,omitempty"`

	// +kubebuilder:validation:Optional
	// configurations to use for  profile specific blockpool.
	BlockPoolConfiguration BlockPoolConfigurationSpec `json:"blockPoolConfiguration,omitempty"`
}

// StorageProfileStatus defines the observed state of StorageProfile
type StorageProfileStatus struct {
	// Phase describes the Phase of StorageProfile
	// This is used by OLM UI to provide status information
	// to the user
	Phase StorageProfilePhase `json:"phase,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// StorageProfile is the Schema for the storageprofiles API
type StorageProfile struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// +kubebuilder:validation:Required
	// +kubebuilder:validation:XValidation:rule="oldSelf == self",message="spec is immutable"
	Spec   StorageProfileSpec   `json:"spec"`
	Status StorageProfileStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// StorageProfileList contains a list of StorageProfile
type StorageProfileList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []StorageProfile `json:"items"`
}

// StorageProfilePhase stores a StorageProfile reconciliation phase
type StorageProfilePhase string

const (
	StorageProfilePhaseRejected StorageProfilePhase = "Rejected"
)

func (sp *StorageProfile) GetSpecHash() string {
	specJSON, err := json.Marshal(sp.Spec)
	if err != nil {
		errStr := fmt.Errorf("failed to marshal StorageProfile.Spec for %s", sp.Name)
		panic(errStr)
	}
	specHash := md5.Sum(specJSON)
	return hex.EncodeToString(specHash[:])
}
