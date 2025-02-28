/*
Copyright 2022 Red Hat OpenShift Container Storage.

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
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	noobaaApis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	"github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	routev1 "github.com/openshift/api/route/v1"
	rookcephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	if err := v1.AddToScheme(scheme); err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	if err := ocsv1alpha1.AddToScheme(scheme); err != nil {
		assert.Fail(t, "failed to add ocsv1alpha1 scheme")
	}
	if err := routev1.AddToScheme(scheme); err != nil {
		assert.Fail(t, "failed to add routev1 scheme")
	}
	if err := noobaaApis.AddToScheme(scheme); err != nil {
		assert.Fail(t, "failed to add nbapis scheme")
	}
	if err := configv1.AddToScheme(scheme); err != nil {
		assert.Fail(t, "failed to add configv1 scheme")
	}
	if err := rookcephv1.AddToScheme(scheme); err != nil {
		assert.Fail(t, "failed to add configv1 scheme")
	}

	return scheme
}

func TestNoobaaAccount(t *testing.T) {
	var r StorageConsumerReconciler
	ctx := context.TODO()
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).Build()
	r.Client = client
	r.Scheme = scheme
	r.Log = log.Log.WithName("controller_storagecluster_test")
	r.storageConsumer = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "provider",
		},
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: true,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{
				{
					Kind:  "NooBaaAccount",
					Name:  "consumer-acc",
					Phase: "Ready",
				},
			},
			Client: ocsv1alpha1.ClientStatus{
				ClusterID: "provider",
			},
		},
	}
	storageCluster := &v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storagecluster",
		},
	}
	err := client.Create(ctx, storageCluster)
	assert.NoError(t, err)
	cephfilesystem := &rookcephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storagecluster-cephfilesystem",
		},
	}
	err = client.Create(ctx, cephfilesystem)
	assert.NoError(t, err)
	noobaaAccount := &v1alpha1.NooBaaAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "provider",
		},
		Status: v1alpha1.NooBaaAccountStatus{
			Phase: v1alpha1.NooBaaAccountPhaseReady,
		},
	}
	err = client.Create(ctx, noobaaAccount)
	assert.NoError(t, err)
	clusterVersionProvider := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Spec: configv1.ClusterVersionSpec{
			ClusterID: "12345",
		},
	}
	err = client.Create(ctx, clusterVersionProvider)
	assert.NoError(t, err)
	_, err = r.reconcilePhases()
	assert.NoError(t, err)

	want := &ocsv1alpha1.CephResourcesSpec{
		Kind:  "NooBaaAccount",
		Name:  "provider",
		Phase: "Ready",
	}

	for i := range r.storageConsumer.Status.CephResources {
		if r.storageConsumer.Status.CephResources[i].Kind == "NooBaaAccount" {
			assert.Equal(t, want, r.storageConsumer.Status.CephResources[i])
		}
	}

	// When StorageConsumer cr status in Error state
	client = fake.NewClientBuilder().WithScheme(scheme).Build()
	r.Client = client

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consumer",
		},
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: true,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{
				{
					Kind:  "NooBaaAccount",
					Name:  "consumer",
					Phase: "Error",
				},
			},
		},
	}
	storageCluster = &v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storagecluster",
		},
	}
	err = client.Create(ctx, storageCluster)
	assert.NoError(t, err)
	cephfilesystem = &rookcephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name: "storagecluster-cephfilesystem",
		},
	}
	err = client.Create(ctx, cephfilesystem)
	assert.NoError(t, err)
	noobaaAccount = &v1alpha1.NooBaaAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: "consumer",
		},
		Status: v1alpha1.NooBaaAccountStatus{
			Phase: v1alpha1.NooBaaAccountPhaseRejected,
		},
	}
	err = client.Create(ctx, noobaaAccount)
	assert.NoError(t, err)
	clusterVersionConsumer := &configv1.ClusterVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name: "version",
		},
		Spec: configv1.ClusterVersionSpec{
			ClusterID: "provider",
		},
	}
	err = client.Create(ctx, clusterVersionConsumer)
	assert.NoError(t, err)
	_, err = r.reconcilePhases()
	assert.NoError(t, err)

	want = &ocsv1alpha1.CephResourcesSpec{
		Kind:  "NooBaaAccount",
		Name:  "consumer",
		Phase: "Rejected",
	}

	for i := range r.storageConsumer.Status.CephResources {
		if r.storageConsumer.Status.CephResources[i].Kind == "NooBaaAccount" {
			assert.Equal(t, want, r.storageConsumer.Status.CephResources[i])
		}
	}
}
