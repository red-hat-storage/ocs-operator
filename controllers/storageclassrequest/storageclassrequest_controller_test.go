/*
Copyright 2023 Red Hat OpenShift Container Storage.
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

package storageclassrequest

import (
	"fmt"
	"strings"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	pgAutoscaleMode = "pg_autoscale_mode"
	pgNum           = "pg_num"
	pgpNum          = "pgp_num"
	namespaceName   = "test-ns"
	deviceClass     = "ssd"
)

var fakeStorageProfile = &v1.StorageProfile{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "medium",
		Namespace: namespaceName,
	},
	Spec: v1.StorageProfileSpec{
		DeviceClass: deviceClass,
	},
}

var validStorageProfile = &v1.StorageProfile{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "valid",
		Namespace: namespaceName,
	},
	Spec: v1.StorageProfileSpec{
		DeviceClass: deviceClass,
		BlockPoolConfiguration: v1.BlockPoolConfigurationSpec{
			Parameters: map[string]string{
				pgAutoscaleMode: "on",
				pgNum:           "128",
				pgpNum:          "128",
			},
		},
	},
	Status: v1.StorageProfileStatus{Phase: ""},
}

var rejectedStorageProfile = &v1.StorageProfile{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "rejected",
		Namespace: namespaceName,
	},
	Spec: v1.StorageProfileSpec{
		DeviceClass: "",
		BlockPoolConfiguration: v1.BlockPoolConfigurationSpec{
			Parameters: map[string]string{
				pgAutoscaleMode: "on",
				pgNum:           "128",
				pgpNum:          "128",
			},
		},
	},
	Status: v1.StorageProfileStatus{Phase: ""},
}

var fakeStorageCluster = &v1.StorageCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-storagecluster",
		Namespace: namespaceName,
	},
	Spec: v1.StorageClusterSpec{
		DefaultStorageProfile: fakeStorageProfile.Name,
		StorageDeviceSets: []v1.StorageDeviceSet{
			{
				DeviceClass: deviceClass,
			},
		},
	},
	Status: v1.StorageClusterStatus{
		FailureDomain: "zone",
	},
}

var fakeStorageConsumer = &v1alpha1.StorageConsumer{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "test-consumer",
		Namespace: namespaceName,
	},
}

var fakeCephFs = &rookCephv1.CephFilesystem{
	ObjectMeta: metav1.ObjectMeta{
		Name:      fmt.Sprintf("%s-cephfilesystem", fakeStorageCluster.Name),
		Namespace: fakeStorageCluster.Namespace,
	},
	Spec: rookCephv1.FilesystemSpec{
		DataPools: []rookCephv1.NamedPoolSpec{
			{
				PoolSpec: rookCephv1.PoolSpec{
					DeviceClass: deviceClass,
				},
			},
		},
	},
}

func createFakeScheme(t *testing.T) *runtime.Scheme {

	scheme, err := v1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}

	err = v1alpha1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsv1alpha1 scheme")
	}

	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1scheme")
	}

	return scheme
}

func createFakeReconciler(t *testing.T) StorageClassRequestReconciler {
	var fakeReconciler StorageClassRequestReconciler

	fakeReconciler.Scheme = createFakeScheme(t)
	fakeReconciler.log = log.Log.WithName("controller_storagecluster_test")
	fakeReconciler.OperatorNamespace = namespaceName
	fakeReconciler.StorageClassRequest = &v1alpha1.StorageClassRequest{}
	fakeReconciler.cephResourcesByName = map[string]*v1alpha1.CephResourcesSpec{}

	fakeReconciler.storageConsumer = fakeStorageConsumer
	fakeReconciler.storageCluster = fakeStorageCluster

	return fakeReconciler
}

func TestStorageProfileCephBlockPool(t *testing.T) {
	var err error
	var caseCounter int

	var primaryTestCases = []struct {
		label            string
		expectedPoolName string
		failureExpected  bool
		createObjects    []runtime.Object
		storageProfile   *v1.StorageProfile
	}{
		{
			label:            "valid profile",
			expectedPoolName: "test-valid-blockpool",
			failureExpected:  false,
			storageProfile:   validStorageProfile,
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-valid-blockpool",
						Namespace: namespaceName,
						Labels: map[string]string{
							controllers.StorageConsumerNameLabel: fakeStorageConsumer.Name,
							controllers.StorageProfileSpecLabel:  validStorageProfile.GetSpecHash(),
						},
					}, Spec: rookCephv1.NamedBlockPoolSpec{
						Name: "spec",
						PoolSpec: rookCephv1.PoolSpec{
							FailureDomain: "zone",
							DeviceClass:   deviceClass,
							Parameters:    map[string]string{},
						},
					},
				},
			},
		},
		{
			label:            "rejected profile",
			expectedPoolName: "test-rejected-blockpool",
			failureExpected:  true,
			storageProfile:   rejectedStorageProfile,
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-rejected-blockpool",
						Namespace: namespaceName,
						Labels: map[string]string{
							controllers.StorageConsumerNameLabel: fakeStorageConsumer.Name,
							controllers.StorageProfileSpecLabel:  rejectedStorageProfile.GetSpecHash(),
						},
					}, Spec: rookCephv1.NamedBlockPoolSpec{
						Name: "spec",
						PoolSpec: rookCephv1.PoolSpec{
							FailureDomain: "zone",
							DeviceClass:   deviceClass,
							Parameters:    map[string]string{},
						},
					},
				},
			},
		},
	}

	for _, c := range primaryTestCases {
		caseCounter++
		caseLabel := fmt.Sprintf("Case %d: %s", caseCounter, c.label)
		fmt.Println(caseLabel)

		r := createFakeReconciler(t)
		if strings.Contains(c.label, "rejected") {
			r.storageCluster.Spec.DefaultStorageProfile = rejectedStorageProfile.Name
		}
		r.StorageClassRequest.Spec.Type = "blockpool"

		r.StorageClassRequest.Spec.StorageProfile = c.storageProfile.Name

		c.createObjects = append(c.createObjects, c.storageProfile)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)

		fakeClient := fake.NewClientBuilder().WithScheme(r.Scheme).WithRuntimeObjects(c.createObjects...)
		r.Client = fakeClient.Build()

		_, err = r.reconcilePhases()
		if c.failureExpected {
			assert.Error(t, err, caseLabel)
			continue
		}
		assert.NoError(t, err, caseLabel)

		assert.Equal(t, c.expectedPoolName, r.cephBlockPool.Name, caseLabel)

		if strings.Contains(c.expectedPoolName, "valid") {
			expectedStorageProfileParameters := validStorageProfile.Spec.BlockPoolConfiguration.Parameters
			actualBlockPoolParameters := r.cephBlockPool.Spec.Parameters
			assert.Equal(t, expectedStorageProfileParameters, actualBlockPoolParameters, caseLabel)
		} else {
			actualBlockPoolParameters := r.cephBlockPool.Spec.Parameters
			assert.Equal(t, v1.StorageProfilePhaseRejected, rejectedStorageProfile.Status.Phase)
			assert.Nil(t, actualBlockPoolParameters, caseLabel)
		}
	}

}

func TestStorageProfileCephFsSubVolGroup(t *testing.T) {
	var err error
	var caseCounter int

	var primaryTestCases = []struct {
		label             string
		expectedGroupName string
		failureExpected   bool
		createObjects     []runtime.Object
		cephResources     []*v1alpha1.CephResourcesSpec
		storageProfile    *v1.StorageProfile
		cephFs            *rookCephv1.CephFilesystem
	}{
		{
			label:             "valid profile",
			expectedGroupName: "test-subvolgroup",
			storageProfile:    fakeStorageProfile,
			cephFs:            fakeCephFs,
			failureExpected:   false,
			cephResources: []*v1alpha1.CephResourcesSpec{
				{
					Name: "test-subvolgroup",
					Kind: "CephFilesystemSubVolumeGroup",
				},
			},
			createObjects: []runtime.Object{
				&rookCephv1.CephFilesystemSubVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-subvolgroup",
						Namespace: namespaceName,
					},
					Status: &rookCephv1.CephFilesystemSubVolumeGroupStatus{},
				},
			},
		},
		{
			label:             "rejected profile",
			expectedGroupName: "test-subvolgroup",
			storageProfile:    rejectedStorageProfile,
			cephFs:            fakeCephFs,
			failureExpected:   true,
			cephResources: []*v1alpha1.CephResourcesSpec{
				{
					Name: "test-subvolgroup",
					Kind: "CephFilesystemSubVolumeGroup",
				},
			},
			createObjects: []runtime.Object{
				&rookCephv1.CephFilesystemSubVolumeGroup{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "test-subvolgroup",
						Namespace: namespaceName,
					},
					Status: &rookCephv1.CephFilesystemSubVolumeGroupStatus{},
				},
			},
		},
	}

	for _, c := range primaryTestCases {
		caseCounter++
		caseLabel := fmt.Sprintf("Case %d: %s", caseCounter, c.label)
		fmt.Println(caseLabel)

		r := createFakeReconciler(t)
		if strings.Contains(c.label, "rejected") {
			r.storageCluster.Spec.DefaultStorageProfile = rejectedStorageProfile.Name
		}

		r.StorageClassRequest.Status.CephResources = c.cephResources
		r.StorageClassRequest.Spec.Type = "sharedfilesystem"
		r.StorageClassRequest.Spec.StorageProfile = c.storageProfile.Name

		c.createObjects = append(c.createObjects, c.cephFs)
		c.createObjects = append(c.createObjects, c.storageProfile)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)
		fakeClient := fake.NewClientBuilder().WithScheme(r.Scheme).WithRuntimeObjects(c.createObjects...)

		r.Client = fakeClient.Build()
		r.StorageClassRequest.Status.CephResources = c.cephResources

		_, err = r.reconcilePhases()
		if c.failureExpected {
			assert.Error(t, err, caseLabel)
			continue
		}
		assert.NoError(t, err, caseLabel)
		assert.Equal(t, c.expectedGroupName, r.cephFilesystemSubVolumeGroup.Name, caseLabel)
	}
}
