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
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache/informertest"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	pgAutoscaleMode        = "pg_autoscale_mode"
	pgNum                  = "pg_num"
	pgpNum                 = "pgp_num"
	namespaceName          = "test-ns"
	deviceClass            = "ssd"
	storageProfileKind     = "StorageProfile"
	storageClassRequestUID = "storageClassRequestUUID"
)

var fakeStorageProfile = &v1.StorageProfile{
	TypeMeta: metav1.TypeMeta{Kind: storageProfileKind},
	ObjectMeta: metav1.ObjectMeta{
		Name:      "medium",
		Namespace: namespaceName,
	},
	Spec: v1.StorageProfileSpec{
		DeviceClass: deviceClass,
	},
}

// A rejected StorageProfile is one that is invalid due to having a blank device class field and is set to
// Rejected in its phase.
var rejectedStorageProfile = &v1.StorageProfile{
	TypeMeta: metav1.TypeMeta{Kind: storageProfileKind},
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

	scheme := runtime.NewScheme()
	err := v1.AddToScheme(scheme)
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
	fakeReconciler.StorageClassRequest = &v1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			UID: storageClassRequestUID,
		},
	}
	fakeReconciler.cephResourcesByName = map[string]*v1alpha1.CephResourcesSpec{}

	fakeReconciler.storageConsumer = fakeStorageConsumer
	fakeReconciler.storageCluster = fakeStorageCluster

	return fakeReconciler
}

func newFakeClientBuilder(scheme *runtime.Scheme) *fake.ClientBuilder {
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithIndex(&rookCephv1.CephBlockPoolRadosNamespace{}, util.OwnerUIDIndexName, util.OwnersIndexFieldFunc).
		WithIndex(&rookCephv1.CephFilesystemSubVolumeGroup{}, util.OwnerUIDIndexName, util.OwnersIndexFieldFunc)
}

func TestProfileReconcile(t *testing.T) {
	var err error
	var caseCounter int

	var primaryTestCases = []struct {
		label           string
		scrType         string
		profileName     string
		failureExpected bool
		createObjects   []runtime.Object
	}{
		{
			label:       "Reconcile sharedfilesystem StorageClassRequest",
			scrType:     "sharedfilesystem",
			profileName: fakeStorageProfile.Name,
		},
		{
			label:   "Reconcile sharedfilesystem StorageClassRequest with default StorageProfile",
			scrType: "sharedfilesystem",
		},
		{
			label:           "Reconcile sharedfilesystem StorageClassRequest with invalid StorageProfile",
			scrType:         "sharedfilesystem",
			profileName:     "nope",
			failureExpected: true,
		},
	}

	for _, c := range primaryTestCases {
		caseCounter++
		caseLabel := fmt.Sprintf("Case %d: %s", caseCounter, c.label)
		fmt.Println(caseLabel)

		r := createFakeReconciler(t)

		fakeStorageClassRequest := &v1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "test-scr",
				Namespace: "test-ns",
			},
			Spec: v1alpha1.StorageClassRequestSpec{
				Type:           c.scrType,
				StorageProfile: c.profileName,
			},
		}
		err = controllerutil.SetOwnerReference(fakeStorageConsumer, fakeStorageClassRequest, r.Scheme)
		assert.NoError(t, err, caseLabel)

		c.createObjects = append(c.createObjects, fakeCephFs)
		c.createObjects = append(c.createObjects, fakeStorageClassRequest)
		c.createObjects = append(c.createObjects, fakeStorageCluster)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)
		c.createObjects = append(c.createObjects, fakeStorageProfile)

		r.Cache = &informertest.FakeInformers{Scheme: r.Scheme}
		fakeClient := newFakeClientBuilder(r.Scheme).
			WithRuntimeObjects(c.createObjects...).
			WithStatusSubresource(fakeStorageClassRequest)
		r.Client = fakeClient.Build()

		req := reconcile.Request{}
		req.Name = fakeStorageClassRequest.Name
		req.Namespace = fakeStorageClassRequest.Namespace
		_, err = r.Reconcile(context.TODO(), req)
		if c.failureExpected {
			assert.Error(t, err, caseLabel)
			continue
		}
		assert.NoError(t, err, caseLabel)
	}

	caseCounter++
	caseLabel := fmt.Sprintf("Case %d: No StorageClassRequest exists", caseCounter)
	fmt.Println(caseLabel)

	r := createFakeReconciler(t)
	r.Cache = &informertest.FakeInformers{Scheme: r.Scheme}
	r.Client = fake.NewClientBuilder().WithScheme(r.Scheme).Build()

	req := reconcile.Request{}
	req.Name = "nope"
	req.Namespace = r.OperatorNamespace
	_, err = r.Reconcile(context.TODO(), req)
	assert.NoError(t, err, caseLabel)
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
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
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

		r.StorageClassRequest.Spec.Type = "sharedfilesystem"
		r.StorageClassRequest.Spec.StorageProfile = c.storageProfile.Name

		c.createObjects = append(c.createObjects, c.cephFs)
		c.createObjects = append(c.createObjects, c.storageProfile)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)
		fakeClient := newFakeClientBuilder(r.Scheme).
			WithRuntimeObjects(c.createObjects...)
		r.Client = fakeClient.Build()

		_, err = r.reconcilePhases()
		if c.failureExpected {
			assert.Error(t, err, caseLabel)
			continue
		}
		assert.NoError(t, err, caseLabel)
		assert.Equal(t, c.expectedGroupName, r.cephFilesystemSubVolumeGroup.Name, caseLabel)
	}
}

func TestCephBlockPool(t *testing.T) {
	var err error
	var caseCounter int

	var primaryTestCases = []struct {
		label            string
		expectedPoolName string
		failureExpected  bool
		createObjects    []runtime.Object
		cephResources    []*v1alpha1.CephResourcesSpec
	}{
		{
			label:           "No CephBlockPool exists",
			failureExpected: true,
		},
		{
			label:            "Valid CephBlockPool and RadosNamespace exist",
			expectedPoolName: "medium",
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium",
						Namespace: "test-ns",
						Labels: map[string]string{
							controllers.StorageProfileSpecLabel: fakeStorageProfile.GetSpecHash(),
						},
					},
				},
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium2",
						Namespace: "test-ns",
						Labels: map[string]string{
							controllers.StorageProfileSpecLabel: "0123456789",
						},
					},
				},
				&rookCephv1.CephBlockPoolRadosNamespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cephradosnamespace-d41d8cd98f00b204e9800998ecf8427e",
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
					},
					Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
						BlockPoolName: "medium",
					},
				},
			},
		},
		{
			label:            "Valid RadosNamespace only exists for different profile",
			expectedPoolName: "medium",
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium",
						Namespace: "test-ns",
						Labels: map[string]string{
							controllers.StorageProfileSpecLabel: "0123456789",
						},
					},
				},
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium2",
						Namespace: "test-ns",
						Labels: map[string]string{
							controllers.StorageProfileSpecLabel: "0123456789",
						},
					},
				},
				&rookCephv1.CephBlockPoolRadosNamespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "cephradosnamespace-medium-test-consumer",
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: "0123456789",
							},
						},
					},
					Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
						BlockPoolName: "medium2",
					},
				},
			},
		},
		{
			label:            "More than one valid RadosNamespace exists",
			failureExpected:  true,
			expectedPoolName: "medium",
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium",
						Namespace: "test-ns",
						Labels: map[string]string{
							controllers.StorageProfileSpecLabel: fakeStorageProfile.GetSpecHash(),
						},
					},
				},
				&rookCephv1.CephBlockPoolRadosNamespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium-rns",
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
					},
					Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
						BlockPoolName: "medium",
					},
				},
				&rookCephv1.CephBlockPoolRadosNamespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium-rns2",
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
					},
					Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
						BlockPoolName: "medium",
					},
				},
			},
		},
		{
			label:            "Request status has existing RadosNamespace and inextant CephBlockPool",
			expectedPoolName: "medium",
			cephResources: []*v1alpha1.CephResourcesSpec{
				{
					Name: "medium",
					Kind: "CephBlockPool",
				},
				{
					Name: "medium-rns",
					Kind: "CephBlockPoolRadosNamespace",
				},
			},
			createObjects: []runtime.Object{
				&rookCephv1.CephBlockPoolRadosNamespace{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "medium-rns",
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
					},
					Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
						BlockPoolName: "medium",
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
		r.StorageClassRequest.Status.CephResources = c.cephResources
		r.StorageClassRequest.Spec.Type = "blockpool"
		r.StorageClassRequest.Spec.StorageProfile = fakeStorageProfile.Name

		c.createObjects = append(c.createObjects, fakeStorageProfile)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)
		fakeClient := newFakeClientBuilder(r.Scheme).WithRuntimeObjects(c.createObjects...)
		r.Client = fakeClient.Build()

		_, err = r.reconcilePhases()

		if err == nil && c.expectedPoolName == "" {
			assert.NotEmpty(t, r.cephRadosNamespace.Spec.BlockPoolName, caseLabel)
			createdBlockpool := &rookCephv1.CephBlockPool{}
			createdBlockpool.Name = r.cephRadosNamespace.Spec.BlockPoolName
			createdBlockpool.Namespace = r.cephRadosNamespace.Namespace

			err = r.get(createdBlockpool)
		}

		if c.failureExpected {
			assert.Error(t, err, caseLabel)
			continue
		}
		assert.NoError(t, err, caseLabel)

		assert.Equal(t, c.expectedPoolName, r.cephRadosNamespace.Spec.BlockPoolName, caseLabel)

		// The generated CephBlockPoolRadosNamespace name is expected
		// to be deterministic, so hard-coding the name generation in
		// the test to guard against unintentional changes.
		md5Sum := md5.Sum([]byte(r.StorageClassRequest.Name))
		expectedRadosNamespaceName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(md5Sum[:16]))
		for _, cephRes := range c.cephResources {
			if cephRes.Kind == "CephBlockPoolRadosNamespace" {
				expectedRadosNamespaceName = cephRes.Name
				break
			}
		}
		expectedRadosNamespace := &rookCephv1.CephBlockPoolRadosNamespace{
			ObjectMeta: metav1.ObjectMeta{
				Name:      expectedRadosNamespaceName,
				Namespace: r.cephRadosNamespace.Namespace,
			},
		}

		assert.NotEmpty(t, r.cephRadosNamespace, caseLabel)
		assert.Equal(t, expectedRadosNamespaceName, r.cephRadosNamespace.Name, caseLabel)

		err = r.get(expectedRadosNamespace)
		assert.NoError(t, err, caseLabel)
	}
}

func TestCephFsSubVolGroup(t *testing.T) {
	var err error
	var caseCounter int

	var primaryTestCases = []struct {
		label             string
		expectedGroupName string
		createObjects     []runtime.Object
		cephResources     []*v1alpha1.CephResourcesSpec
	}{
		{
			label: "No CephFilesystemSubVolumeGroup exists",
		},
		{
			label:             "CephFilesystemSubVolumeGroup already has valid ownerReference",
			expectedGroupName: "test-subvolgroup",
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
						Namespace: "test-ns",
						OwnerReferences: []metav1.OwnerReference{
							{
								UID: storageClassRequestUID,
							},
						},
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
		r.StorageClassRequest.Spec.Type = "sharedfilesystem"
		r.StorageClassRequest.Spec.StorageProfile = fakeStorageProfile.Name

		c.createObjects = append(c.createObjects, fakeCephFs)
		c.createObjects = append(c.createObjects, fakeStorageProfile)
		c.createObjects = append(c.createObjects, fakeStorageConsumer)
		fakeClient := newFakeClientBuilder(r.Scheme).
			WithRuntimeObjects(c.createObjects...)
		r.Client = fakeClient.Build()

		_, err = r.reconcilePhases()
		assert.NoError(t, err, caseLabel)

		if c.expectedGroupName == "" {
			assert.NotEmpty(t, r.cephFilesystemSubVolumeGroup, caseLabel)
			createdSubVolGroup := &rookCephv1.CephFilesystemSubVolumeGroup{}
			createdSubVolGroup.Name = r.cephFilesystemSubVolumeGroup.Name
			createdSubVolGroup.Namespace = r.cephFilesystemSubVolumeGroup.Namespace

			err = r.get(createdSubVolGroup)
			assert.NoError(t, err, caseLabel)
		} else {
			assert.Equal(t, c.expectedGroupName, r.cephFilesystemSubVolumeGroup.Name, caseLabel)
		}
	}

	caseCounter++
	caseLabel := fmt.Sprintf("Case %d: No CephFilesystem exists", caseCounter)
	fmt.Println(caseLabel)

	r := createFakeReconciler(t)
	r.StorageClassRequest.Spec.Type = "sharedfilesystem"
	r.StorageClassRequest.Spec.StorageProfile = fakeStorageProfile.Name
	fakeClient := newFakeClientBuilder(r.Scheme).
		WithRuntimeObjects(fakeStorageProfile, fakeStorageConsumer)
	r.Client = fakeClient.Build()

	_, err = r.reconcilePhases()
	assert.Error(t, err, caseLabel)

	caseCounter++
	caseLabel = fmt.Sprintf("Case %d: StorageProfile has invalid DeviceClass", caseCounter)
	fmt.Println(caseLabel)

	badStorageProfile := fakeStorageProfile.DeepCopy()
	badStorageProfile.Spec.DeviceClass = "nope"

	r = createFakeReconciler(t)
	r.StorageClassRequest.Spec.Type = "sharedfilesystem"
	r.StorageClassRequest.Spec.StorageProfile = badStorageProfile.Name
	fakeClient = newFakeClientBuilder(r.Scheme)
	r.Client = fakeClient.WithRuntimeObjects(badStorageProfile, fakeStorageConsumer, fakeCephFs).Build()

	_, err = r.reconcilePhases()
	assert.Error(t, err, caseLabel)
}
