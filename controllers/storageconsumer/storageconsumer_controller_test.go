/*
Copyright 2021 Red Hat OpenShift Container Storage.

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

package controllers

import (
	"context"
	"fmt"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	failureDomain = "Zone"
)

var mockStorageConsumerRequest = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Name:      "storageconsumer-test",
		Namespace: "storage-test-ns",
	},
}

var MockStorageConsumer = &ocsv1alpha1.StorageConsumer{
	ObjectMeta: metav1.ObjectMeta{
		Name:      mockStorageConsumerRequest.Name,
		Namespace: mockStorageConsumerRequest.Namespace,
	},
	Spec: ocsv1alpha1.StorageConsumerSpec{
		Capacity: resource.MustParse("1Ti"),
	},
}

var mockStorageCluster = &ocsv1.StorageCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "storagecluster-test",
		Namespace: mockStorageConsumerRequest.Namespace,
	},
	Spec: ocsv1.StorageClusterSpec{},
	Status: ocsv1.StorageClusterStatus{
		FailureDomain: failureDomain,
	},
}

var mockCephFilesystem = &rookCephv1.CephFilesystem{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cephfilesystem-test",
		Namespace: mockStorageConsumerRequest.Namespace,
	},
}

func getReconciler(t *testing.T, objs ...client.Object) StorageConsumerReconciler {
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	log := logf.Log.WithName("controller_storagecluster_test")

	return StorageConsumerReconciler{
		Scheme: scheme,
		Client: client,
		Log:    log,
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := ocsv1alpha1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}

	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}

	err = ocsv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsv1 scheme")
	}

	return scheme
}

func TestEnsureExternalStorageClusterResources(t *testing.T) {

	reconciler := getReconciler(t)
	_ = reconciler.Client.Create(context.TODO(), MockStorageConsumer)
	_, err := reconciler.Reconcile(context.TODO(), mockStorageConsumerRequest)
	assert.Error(t, err)

	_ = reconciler.Client.Create(context.TODO(), mockStorageCluster)
	_, err = reconciler.Reconcile(context.TODO(), mockStorageConsumerRequest)
	assert.Error(t, err)

	_ = reconciler.Client.Create(context.TODO(), mockCephFilesystem)
	_, err = reconciler.Reconcile(context.TODO(), mockStorageConsumerRequest)
	assert.NoError(t, err)

	assertCephBlockPool(t, reconciler)
	assertCephFilesystemSubVolumeGroup(t, reconciler)
	assertCephClients(t, reconciler)

}

func assertCephBlockPool(t *testing.T, reconciler StorageConsumerReconciler) {

	assert.Equal(t, reconciler.cephBlockPool.Name, fmt.Sprintf("%s-%s", "cephblockpool", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephBlockPool.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephBlockPool.Spec.FailureDomain, failureDomain)
	replicatedSpec := rookCephv1.ReplicatedSpec{
		Size:                     3,
		ReplicasPerFailureDomain: 1,
	}
	assert.Equal(t, reconciler.cephBlockPool.Spec.Replicated, replicatedSpec)
	Parameters := map[string]string{
		"target_size_ratio": ".49",
	}
	assert.Equal(t, reconciler.cephBlockPool.Spec.Parameters, Parameters)

	capacity := reconciler.storageConsumer.Spec.Capacity.String()
	assert.Equal(t, reconciler.cephBlockPool.Spec.Quotas.MaxSize, &capacity)

}

func assertCephFilesystemSubVolumeGroup(t *testing.T, reconciler StorageConsumerReconciler) {

	assert.Equal(t, reconciler.cephFilesystemSubVolumeGroup.Name, fmt.Sprintf("%s-%s", "cephfilesystemsubvolumegroup", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephFilesystemSubVolumeGroup.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephFilesystemSubVolumeGroup.Spec.FilesystemName, mockCephFilesystem.Name)

}

func assertCephClients(t *testing.T, reconciler StorageConsumerReconciler) {
	desiredRBDProvisionerCaps := map[string]string{
		"mon": "profile rbd",
		"mgr": "allow rw",
		"osd": fmt.Sprintf("profile rbd pool=%s", reconciler.cephBlockPool.Name),
	}
	assert.Equal(t, reconciler.cephClientRBDProvisioner.Name, fmt.Sprintf("%s-%s", "cephclient-rbd-provisioner", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephClientRBDProvisioner.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephClientRBDProvisioner.Spec.Caps, desiredRBDProvisionerCaps)

	desiredRBDNodeCaps := map[string]string{
		"mon": "profile rbd",
		"mgr": "allow rw",
		"osd": fmt.Sprintf("profile rbd pool=%s", reconciler.cephBlockPool.Name),
	}
	assert.Equal(t, reconciler.cephClientRBDNode.Name, fmt.Sprintf("%s-%s", "cephclient-rbd-node", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephClientRBDNode.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephClientRBDNode.Spec.Caps, desiredRBDNodeCaps)

	desiredCephFSProvisionerCaps := map[string]string{
		"mon": "allow r",
		"mgr": "allow rw",
		"mds": fmt.Sprintf("allow rw path=/volumes/%s", reconciler.cephFilesystemSubVolumeGroup.Name),
		"osd": "allow rw tag cephfs metadata=*",
	}
	assert.Equal(t, reconciler.cephClientCephFSProvisioner.Name, fmt.Sprintf("%s-%s", "cephclient-cephfs-provisioner", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephClientCephFSProvisioner.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephClientCephFSProvisioner.Spec.Caps, desiredCephFSProvisionerCaps)

	desiredCephFSNodeCaps := map[string]string{
		"mon": "allow r",
		"mgr": "allow rw",
		"osd": "allow rw tag cephfs *=*",
		"mds": fmt.Sprintf("allow rw path=/volumes/%s", reconciler.cephFilesystemSubVolumeGroup.Name),
	}
	assert.Equal(t, reconciler.cephClientCephFSNode.Name, fmt.Sprintf("%s-%s", "cephclient-cephfs-node", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephClientCephFSNode.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephClientCephFSNode.Spec.Caps, desiredCephFSNodeCaps)

	desiredHealthCheckerCaps := map[string]string{
		"mgr": "allow command config",
		"mon": "allow r, allow command quorum_status, allow command version",
	}
	assert.Equal(t, reconciler.cephClientHealthChecker.Name, fmt.Sprintf("%s-%s", "cephclient-health-checker", reconciler.storageConsumer.Name))
	assert.Equal(t, len(reconciler.cephClientHealthChecker.OwnerReferences), 1)
	assert.Equal(t, reconciler.cephClientHealthChecker.Spec.Caps, desiredHealthCheckerCaps)
}
