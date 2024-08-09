/*
Copyright 2024.

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

// BlockPoolRefSpec identify a blockpool - client profile pair
type BlockPoolRefSpec struct {
	//+kubebuilder:validation:Required
	ClientProfileName string `json:"clientProfileName,omitempty"`

	//+kubebuilder:validation:Required
	//+kubebuilder:validation:Minimum:=0
	PoolId int `json:"poolId,omitempty"`
}

// BlockPoolMappingSpec define a mapiing between a local and remote block pools
type BlockPoolMappingSpec struct {
	//+kubebuilder:validation:Required
	Local BlockPoolRefSpec `json:"local,omitempty"`

	//+kubebuilder:validation:Required
	Remote BlockPoolRefSpec `json:"remote,omitempty"`
}

// ClientProfileMappingSpec defines the desired state of ClientProfileMapping
type ClientProfileMappingSpec struct {
	//+kubebuilder:validation:Optional
	BlockPoolMapping []BlockPoolMappingSpec `json:"blockPoolMapping,omitempty"`
}

// ClientProfileMappingStatus defines the observed state of ClientProfileMapping
type ClientProfileMappingStatus struct {
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// ClientProfileMapping is the Schema for the clientprofilemappings API
type ClientProfileMapping struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ClientProfileMappingSpec   `json:"spec,omitempty"`
	Status ClientProfileMappingStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ClientProfileMappingList contains a list of ClientProfileMapping
type ClientProfileMappingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ClientProfileMapping `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ClientProfileMapping{}, &ClientProfileMappingList{})
}
