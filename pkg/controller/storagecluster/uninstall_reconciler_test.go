package storagecluster

import (
	"testing"

	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/stretchr/testify/assert"
)

func TestReconcileUninstallAnnotations(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

		// verify it set default value when nothing is set
		assertStorageClusterUninstallAnnotation(t, reconciler, sc, CleanupPolicyDelete, UninstallModeGraceful)

		// verify it does not return error when there is no update required
		assertStorageClusterUninstallAnnotation(t, reconciler, sc, CleanupPolicyDelete, UninstallModeGraceful)

		// verify it corrects wrong value
		sc.ObjectMeta.Annotations[UninstallModeAnnotation] = "blablabla"
		sc.ObjectMeta.Annotations[CleanupPolicyAnnotation] = "blablabla"
		assertStorageClusterUninstallAnnotation(t, reconciler, sc, CleanupPolicyDelete, UninstallModeGraceful)

		// verify it does not change if !default value is set
		sc.ObjectMeta.Annotations[UninstallModeAnnotation] = string(UninstallModeForced)
		sc.ObjectMeta.Annotations[CleanupPolicyAnnotation] = string(CleanupPolicyRetain)
		assertStorageClusterUninstallAnnotation(t, reconciler, sc, CleanupPolicyRetain, UninstallModeForced)
	}
}

func assertStorageClusterUninstallAnnotation(
	t *testing.T, reconciler ReconcileStorageCluster, sc *api.StorageCluster,
	CleanupPolicy CleanupPolicyType, UninstallMode UninstallModeType) {

	err := reconciler.reconcileUninstallAnnotations(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	if val, found := sc.ObjectMeta.Annotations[UninstallModeAnnotation]; !found {
		assert.FailNow(t, "UninstallModeAnnotation not found")
	} else {
		assert.Equal(t, string(UninstallMode), val)
	}

	if val, found := sc.ObjectMeta.Annotations[CleanupPolicyAnnotation]; !found {
		assert.FailNow(t, "CleanupPolicyAnnotation not found")
	} else {
		assert.Equal(t, string(CleanupPolicy), val)
	}
}
