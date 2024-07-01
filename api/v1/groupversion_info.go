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

// Package v1 contains API Schema definitions for the ocs v1 API group
// +kubebuilder:object:generate=true
// +groupName=ocs.openshift.io
package v1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

var (
	// GroupVersion is group version used to register these objects
	GroupVersion = schema.GroupVersion{Group: "ocs.openshift.io", Version: "v1"}

	// SchemeBuilder points to a list of functions added to Scheme.
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)

	localSchemeBuilder = &SchemeBuilder

	// AddToScheme is a common registration function for mapping packaged scoped group & version keys to a scheme.
	AddToScheme = localSchemeBuilder.AddToScheme
)

// Adds the list of known types to scheme.
func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(GroupVersion,
		&OCSInitialization{}, &OCSInitializationList{},
		&StorageCluster{}, &StorageClusterList{},
		&StorageProfile{}, &StorageProfileList{},
		&StorageClusterPeer{}, &StorageClusterPeerList{},
	)
	metav1.AddToGroupVersion(scheme, GroupVersion)
	return nil
}
