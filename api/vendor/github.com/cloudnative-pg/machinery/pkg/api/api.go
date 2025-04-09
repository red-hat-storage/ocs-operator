/*
Copyright The CloudNativePG Contributors

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

// +genclient
// +kubebuilder:object:root=true
// +k8s:deepcopy-gen=package

package api

// SecretKeySelector contains enough information to let you locate
// the key of a Secret
type SecretKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}

// LocalObjectReference contains enough information to let you locate a
// local object with a known type inside the same namespace
type LocalObjectReference struct {
	// Name of the referent.
	Name string `json:"name"`
}

// ConfigMapKeySelector contains enough information to let you locate
// the key of a ConfigMap
type ConfigMapKeySelector struct {
	// The name of the secret in the pod's namespace to select from.
	LocalObjectReference `json:",inline"`
	// The key to select
	Key string `json:"key"`
}
