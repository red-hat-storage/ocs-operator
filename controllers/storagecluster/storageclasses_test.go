package storagecluster

import (
	"context"
	"testing"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/stretchr/testify/assert"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var (
	dummyKmsAddress  = "http://localhost:3053"
	dummyKmsProvider = "vault"
	customSpec       = &api.StorageClusterSpec{
		Encryption: api.EncryptionSpec{
			StorageClass: true,
			KeyManagementService: api.KeyManagementServiceSpec{
				Enable: true,
			},
		},
	}
	customEncryptedSCNameSpec = &api.StorageClusterSpec{
		Encryption: api.EncryptionSpec{
			StorageClass:     true,
			StorageClassName: "custom-ceph-rbd-encrypted",
			KeyManagementService: api.KeyManagementServiceSpec{
				Enable: true,
			},
		},
	}
	customSCNameSpec = &api.StorageClusterSpec{
		NFS: &api.NFSSpec{
			StorageClassName: "custom-ceph-nfs",
		},
		ManagedResources: api.ManagedResourcesSpec{
			CephBlockPools: api.ManageCephBlockPools{
				StorageClassName: "custom-ceph-rbd",
			},
			CephFilesystems: api.ManageCephFilesystems{
				StorageClassName: "custom-cephfs",
			},
			CephNonResilientPools: api.ManageCephNonResilientPools{
				StorageClassName: "custom-ceph-non-resilient-rbd",
			},
			CephObjectStores: api.ManageCephObjectStores{
				StorageClassName: "custom-ceph-rgw",
			},
		},
	}
	createVirtualMachineCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "virtualmachines"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "kubevirt.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "kubevirt.io",
				Scope: extv1.NamespaceScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "VirtualMachine",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1",
						Served: true,
					},
				},
			},
		}
	}
	createStorageClientCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "storageclients"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "ocs.openshift.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "ocs.openshift.io",
				Scope: extv1.ClusterScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "StorageClient",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1alpha1",
						Served: true,
					},
				},
			},
		}
	}
	createVolumeGroupSnapshotClassCRD = func() *extv1.CustomResourceDefinition {
		pluralName := "volumegroupsnapshotclasses"
		return &extv1.CustomResourceDefinition{
			TypeMeta: metav1.TypeMeta{
				Kind:       "CustomResourceDefinition",
				APIVersion: extv1.SchemeGroupVersion.String(),
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: pluralName + "." + "groupsnapshot.storage.k8s.io",
				UID:  "uid",
			},
			Spec: extv1.CustomResourceDefinitionSpec{
				Group: "groupsnapshot.storage.k8s.io",
				Scope: extv1.ClusterScoped,
				Names: extv1.CustomResourceDefinitionNames{
					Plural: pluralName,
					Kind:   "VolumeGroupSnapshotClass",
				},
				Versions: []extv1.CustomResourceDefinitionVersion{
					{
						Name:   "v1beta1",
						Served: true,
					},
				},
			},
		}
	}
)

func TestDefaultStorageClasses(t *testing.T) {
	testStorageClasses(t, false, nil)
}

func TestCustomStorageClasses(t *testing.T) {
	testStorageClasses(t, false, customSCNameSpec)
}

func TestEncryptedStorageClass(t *testing.T) {
	testStorageClasses(t, true, customSpec)
}

func TestCustomEncryptedStorageClasses(t *testing.T) {
	testStorageClasses(t, true, customEncryptedSCNameSpec)
}

func testStorageClasses(t *testing.T, pvEncryption bool, customSpec *api.StorageClusterSpec) {
	runtimeObjs := []client.Object{}
	if pvEncryption {
		runtimeObjs = append(runtimeObjs, createDummyKMSConfigMap(dummyKmsProvider, dummyKmsAddress, ""))
	}
	t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTest(t, runtimeObjs, customSpec)
	assertStorageClasses(t, reconciler, cr, request)
}

func assertStorageClasses(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	pvEncryption := cr.Spec.Encryption.StorageClass && cr.Spec.Encryption.KeyManagementService.Enable
	scNameCephfs := GenerateNameForCephFilesystemSC(cr)
	scNameNfs := generateNameForCephNetworkFilesystemSC(cr)
	scNameRbd := GenerateNameForCephBlockPoolSC(cr)
	scNameEncryptedRbd := generateNameForEncryptedCephBlockPoolSC(cr)
	scNameRgw := generateNameForCephRgwSC(cr)
	scNameVirt := GenerateNameForCephBlockPoolVirtualizationSC(cr)

	actual := map[string]*storagev1.StorageClass{
		scNameCephfs:       {},
		scNameNfs:          {},
		scNameRbd:          {},
		scNameEncryptedRbd: {},
		scNameRgw:          {},
		scNameVirt:         {},
	}
	expected, err := reconciler.newStorageClassConfigurations(cr)
	assert.NoError(t, err)

	// on a cloud platform, 'Get' should throw an error,
	// as RGW StorageClass won't be created
	skip, skipErr := platform.PlatformsShouldSkipObjectStore()
	assert.NoError(t, skipErr)

	if skip {
		if pvEncryption {
			assert.Equal(t, len(expected), 5)
		} else {
			assert.Equal(t, len(expected), 4)
		}
	} else {
		if pvEncryption {
			assert.Equal(t, len(expected), 6)
		} else {
			// if not a cloud platform, RGW StorageClass should be created/updated
			assert.Equal(t, len(expected), 5)
		}
	}

	for _, scConfig := range expected {
		scName := scConfig.storageClass.Name
		request.Name = scName
		err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actual[scName])
		if skip && scName == scNameRgw {
			assert.Error(t, err)
		} else {
			assert.NoError(t, err)
			actualSc := actual[scName]
			expectedSc := scConfig.storageClass

			// The created StorageClasses should not have any ownerReferences set. Any
			// OwnerReference set will be a cross-namespace OwnerReference, which could
			// lead to other child resources getting GCd.
			// Ref: https://bugzilla.redhat.com/show_bug.cgi?id=1755623
			// Ref: https://bugzilla.redhat.com/show_bug.cgi?id=1691546
			assert.Equal(t, len(actualSc.OwnerReferences), 0)

			assert.Equal(t, scName, actualSc.ObjectMeta.Name)
			assert.Equal(t, expectedSc.Provisioner, actualSc.Provisioner)
			assert.Equal(t, expectedSc.ReclaimPolicy, actualSc.ReclaimPolicy)
			assert.Equal(t, expectedSc.Parameters, actualSc.Parameters)
			if scName == scNameRgw {
				// Doing a bit more validation for the RGW SC since some fields differ whether
				// we do independent or converged mode, typically "objectStoreName" param must exist
				assert.NotEmpty(t, actualSc.Parameters["objectStoreName"], actualSc.Parameters)
				assert.NotEmpty(t, actualSc.Parameters["region"], actualSc.Parameters)
				assert.Equal(t, 3, len(actualSc.Parameters))
			}
			if scName == scNameEncryptedRbd {
				// adding this validation as the following annotations must be there for default encrypted rbd sc
				assert.NotEmpty(t, actualSc.ObjectMeta.Annotations)
				assert.Equal(t, actualSc.ObjectMeta.Annotations["cdi.kubevirt.io/clone-strategy"], "copy")
				assert.Equal(t, actualSc.Parameters["encrypted"], "true")
				assert.NotEmpty(t, actualSc.Parameters["encryptionKMSID"])
			}
			if scName == scNameVirt {
				assert.Equal(t, actualSc.ObjectMeta.Annotations["storageclass.kubevirt.io/is-default-virt-class"], "true")
			}
		}
	}
}
