package storagecluster

import (
	"context"
	"testing"

	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/types"
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

func TestSetRookUninstallandCleanupPolicy(t *testing.T) {

	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}
		t, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

		// there are two annotations which will be 4 combinations, test all 4 combinations

		// set default uninstall annotations
		err := reconciler.reconcileUninstallAnnotations(sc, reconciler.reqLogger)
		assert.NoError(t, err)

		combinationsList := []struct {
			CleanupPolicy             CleanupPolicyType
			UninstallMode             UninstallModeType
			CleanupPolicyConfirmation cephv1.CleanupConfirmationProperty
			AllowUninstallWithVolumes bool
		}{
			{CleanupPolicyDelete, UninstallModeGraceful, cephv1.DeleteDataDirOnHostsConfirmation, false},
			{CleanupPolicyRetain, UninstallModeForced, cephv1.CleanupConfirmationProperty(""), true},
			{CleanupPolicyRetain, UninstallModeGraceful, cephv1.CleanupConfirmationProperty(""), false},
			{CleanupPolicyDelete, UninstallModeForced, cephv1.DeleteDataDirOnHostsConfirmation, true},
		}

		for _, obj := range combinationsList {
			sc.ObjectMeta.Annotations[CleanupPolicyAnnotation] = string(obj.CleanupPolicy)
			sc.ObjectMeta.Annotations[UninstallModeAnnotation] = string(obj.UninstallMode)

			// verify it set the cleanup policy and uninstall mode on cephCluster wrt annotations
			assertCephClusterCleanupPolicy(t, reconciler, sc, obj.CleanupPolicyConfirmation, obj.AllowUninstallWithVolumes)
		}
	}
}

func assertCephClusterCleanupPolicy(
	t *testing.T, reconciler ReconcileStorageCluster, sc *api.StorageCluster,
	CleanupPolicyConfirmation cephv1.CleanupConfirmationProperty, AllowUninstallWithVolumes bool) {

	// verify it set the cleanup policy and uninstall mode on cephCluster wrt annotations
	err := reconciler.setRookUninstallandCleanupPolicy(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	cephCluster := &cephv1.CephCluster{}
	err = reconciler.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	assert.NoError(t, err)

	assert.Equal(t, CleanupPolicyConfirmation, cephCluster.Spec.CleanupPolicy.Confirmation)
	assert.Equal(t, AllowUninstallWithVolumes, cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes)
}
