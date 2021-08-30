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

package api

import (
	corev1 "k8s.io/api/core/v1"

	"github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
)

// All provisioners must implement the Provisioner interface which defines the
// methods used to create and delete new buckets, and to grant or revoke access
// to buckets within the object store.
type Provisioner interface {
	// Provision should be implemented to handle creation of object storage buckets.
	// Provision should NOT create the ObjectBucket resource. ObjectBucket resource creation is done
	// by this library's controller.
	// The Provision implementation must return an ObjectBucket struct with at least the Connection
	// spec filled in. All other ObjectBucket details will be filled in by this library's controller
	// before the ObjectBucket resource is created.
	// The Provision implementation may opt to specify the ObjectBucket spec's ReclaimPolicy in
	// cases where the provisioner wishes to set a different value from the one specified in the
	// ObjectBucketClaim's StorageClass.
	// The Provision implementation should clean up its own resources and return a nil ObjectBucket
	// struct when returning an error. However, if Provision returns an error, it may optionally
	// return a non-nil, non-empty ObjectBucket struct. In this case, this library's controller will
	// specify the ObjectBucket's Name, StorageClassName, ReclaimPolicy, and Labels and then call
	// Delete() with the ObjectBucket to clean up resources.
	Provision(options *BucketOptions) (*v1alpha1.ObjectBucket, error)
	// Grant should be implemented to handle access to existing buckets
	Grant(options *BucketOptions) (*v1alpha1.ObjectBucket, error)
	// Update should be implemented to handle cases when additional config are modified
	Update(ob *v1alpha1.ObjectBucket) error
	// Delete should be implemented to handle bucket deletion
	Delete(ob *v1alpha1.ObjectBucket) error
	// Revoke should be implemented to handle removing bucket access
	Revoke(ob *v1alpha1.ObjectBucket) error
}

// BucketOptions wraps all pertinent data that the Provisioner requires to create a
// bucket and the Reconciler requires to abstract that bucket in kubernetes
type BucketOptions struct {
	// ReclaimPolicy is the reclaimPolicy of the OBC's storage class
	ReclaimPolicy *corev1.PersistentVolumeReclaimPolicy
	// BucketName is the name of the bucket within the object store
	BucketName string
	// ObjectBucketClaim is a copy of the reconciler's OBC
	ObjectBucketClaim *v1alpha1.ObjectBucketClaim
	// Parameters is a complete copy of the OBC's storage class Parameters field
	Parameters map[string]string
}
