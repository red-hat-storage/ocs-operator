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
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	err := v1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}

	err = ocsv1alpha1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsv1alpha1 scheme")
	}

	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1scheme")
	}

	return scheme
}

func TestCephName(t *testing.T) {
	var r StorageConsumerReconciler
	r.cephClientHealthChecker = &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: "healthchecker",
		},
		Status: &rookCephv1.CephClientStatus{
			Phase: rookCephv1.ConditionType(util.PhaseReady),
		},
	}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(r.cephClientHealthChecker).Build()

	r.Client = client
	r.Scheme = scheme
	r.Log = log.Log.WithName("controller_storagecluster_test")

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: true,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{
				{
					Kind:  "CephClient",
					Name:  "healthchecker",
					Phase: "Ready",
				},
				{
					Kind:  "CephClient",
					Name:  "rbd",
					Phase: "Ready",
				},
				{
					Kind:  "CephClient",
					Name:  "cephfs",
					Phase: "Ready",
				},
			},
		},
	}
	_, err := r.reconcilePhases()
	assert.NoError(t, err)

	want := []*ocsv1alpha1.CephResourcesSpec{
		{
			Kind:  "CephClient",
			Name:  "healthchecker",
			Phase: "Ready",
		},
	}
	assert.Equal(t, r.storageConsumer.Status.CephResources, want)

	// When StorageConsumer cr status in Error state
	r.cephClientHealthChecker = &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name: "healthchecker",
		},
		Status: &rookCephv1.CephClientStatus{
			Phase: rookCephv1.ConditionType(util.PhaseError),
		},
	}
	client = fake.NewClientBuilder().WithScheme(scheme).WithObjects(r.cephClientHealthChecker).Build()
	r.Client = client

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: true,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{
				{
					Kind:  "CephClient",
					Name:  "rbd",
					Phase: "Error",
				},
				{
					Kind:  "CephClient",
					Name:  "cephfs",
					Phase: "Ready",
				},
				{
					Kind:  "CephClient",
					Name:  "healthchecker",
					Phase: "Error",
				},
			},
		},
	}
	_, err = r.reconcilePhases()
	assert.NoError(t, err)

	want = []*ocsv1alpha1.CephResourcesSpec{
		{
			Kind:  "CephClient",
			Name:  "healthchecker",
			Phase: "Error",
		},
	}
	assert.Equal(t, r.storageConsumer.Status.CephResources, want)
}
