/*
Copyright 2019 Red Hat Inc.

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
	"k8s.io/apimachinery/pkg/runtime/schema"
)

const ObjectBucketClaimKind = "ObjectBucketClaim"

func ObjectBucketClaimGVK() schema.GroupVersionKind {
	return GroupKindVersion(ObjectBucketClaimKind)
}

// BucketCannedACL strictly types pre-defined S3 bucket ACLs.  Provisioners are recommended to constrain these ACLs
// scoped to the unique bucket that was created for the request. They are a subset of canned ACLs from AWS S3's
// definitions of canned ACLs at https://docs.aws.amazon.com/AmazonS3/latest/dev/acl-overview.html#canned-acl
type BucketCannedACL string

const (
	// BucketCannedACLPrivate owner gets FULL_CONTROL. No one else has access rights.
	BucketCannedACLPrivate BucketCannedACL = "private"
	// BucketCannedACLPublicRead owner gets FULL_CONTROL. All users in the store have read access
	BucketCannedACLPublicRead BucketCannedACL = "public-read"
	// BucketCannedACLPublicReadWrite Owner gets FULL_CONTROL. All users in the store get READ and WRITE access. Granting
	// this on a bucket is generally not recommended.
	BucketCannedACLPublicReadWrite BucketCannedACL = "public-read-write"
)

// ObjectBucketClaimSpec defines the desired state of ObjectBucketClaim
type ObjectBucketClaimSpec struct {
	// StorageClass names the StorageClass object representing the desired provisioner and parameters
	StorageClassName string `json:"storageClassName"`
	// BucketName (not recommended) the name of the bucket.  Caution!
	// In-store bucket names may collide across namespaces.  If you define
	// the name yourself, try to make it as unique as possible.
	BucketName string `json:"bucketName,omityempty"`
	// GenerateBucketName (recommended) a prefix for a bucket name to be
	// followed by a hyphen and 5 random characters. Protects against
	// in-store name collisions.
	GenerateBucketName string `json:"generateBucketName,omitempty"`
	// SSL whether connection to the bucket requires SSL Authentication or not
	SSL bool `json:"ssl"`
	// Generic predefined bucket ACLs for use by provisioners
	// Available BucketCannedACLs are:
	//    BucketCannedACLPrivate
	//    BucketCannedACLPublicRead
	//    BucketCannedACLPublicReadWrite
	//    BucketCannedACLAuthenticatedRead
	BucketCannedACL BucketCannedACL `json:"cannedBucketAcl"`
	// Versioned determines if versioning is enabled
	Versioned bool `json:"versioned"`
	// AdditionalConfig gives providers a location to set
	// proprietary config values (tenant, namespace, etc)
	AdditionalConfig map[string]string `json:"additionalConfig"`
	// ObjectBucketName is the name of the object bucket resource.  This is the authoritative
	// determintaion for binding.
	ObjectBucketName string
}

// ObjectBucketClaimStatusPhase is set by the controller to save the state of the provisioning process.
type ObjectBucketClaimStatusPhase string

const (
	// ObjectBucketClaimStatusPhasePending indicates that the provisioner has begun handling the request and that it is
	// still in process
	ObjectBucketClaimStatusPhasePending = "pending"
	// ObjectBucketClaimStatusPhaseBound indicates that provisioning has succeeded, the objectBucket is marked bound, and
	// there is now a configMap and secret containing the appropriate bucket data in the namespace of the claim
	ObjectBucketClaimStatusPhaseBound = "bound"
	// ObjectBucketClaimStatusPhaseReleased TODO this would likely mean that the OB was deleted. That situation should never
	// happen outside of the claim being deleted.  So this state shouldn't naturally arise out of automation.
	ObjectBucketClaimStatusPhaseReleased = "released"
	// ObjectBucketClaimStatusPhaseFailed indicates that provisioning failed.  There should be no configMap, secret, or
	// object bucket and no bucket should be left hanging in the object store
	ObjectBucketClaimStatusPhaseFailed = "failed"
)

// ObjectBucketClaimStatus defines the observed state of ObjectBucketClaim
type ObjectBucketClaimStatus struct {
	Phase ObjectBucketClaimStatusPhase
}

// +genclient
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
// +k8s:openapi-gen=true

// ObjectBucketClaim is the Schema for the objectbucketclaims API
type ObjectBucketClaim struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ObjectBucketClaimSpec   `json:"spec,omitempty"`
	Status ObjectBucketClaimStatus `json:"status,omitempty"`
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// ObjectBucketClaimList contains a list of ObjectBucketClaim
type ObjectBucketClaimList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ObjectBucketClaim `json:"items"`
}