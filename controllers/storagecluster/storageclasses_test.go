package storagecluster

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/stretchr/testify/assert"
	storagev1 "k8s.io/api/storage/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var allPlatforms = append(SkipObjectStorePlatforms,
	configv1.NonePlatformType, configv1.PlatformType("NonCloudPlatform"), configv1.IBMCloudPlatformType)

func TestStorageClasses(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
		t, reconciler, cr, request := initStorageClusterResourceCreateUpdateTestWithPlatform(
			t, cp, nil, nil)
		assertStorageClasses(t, reconciler, cr, request)
	}

}

func assertStorageClasses(t *testing.T, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	scNameCephfs := generateNameForCephFilesystemSC(cr)
	scNameRbd := generateNameForCephBlockPoolSC(cr)
	scNameRgw := generateNameForCephRgwSC(cr)

	actual := map[string]*storagev1.StorageClass{
		scNameCephfs: {},
		scNameRbd:    {},
		scNameRgw:    {},
	}
	expected, err := reconciler.newStorageClassConfigurations(cr)
	assert.NoError(t, err)

	// on a cloud platform, 'Get' should throw an error,
	// as RGW StorageClass won't be created
	skip, skipErr := reconciler.PlatformsShouldSkipObjectStore()
	assert.NoError(t, skipErr)
	if skip {
		assert.Equal(t, len(expected), 2)
	} else {
		// if not a cloud platform, RGW StorageClass should be created/updated
		assert.Equal(t, len(expected), 3)
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
		}
	}
}
