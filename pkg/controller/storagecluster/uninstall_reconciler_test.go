package storagecluster

import (
	"context"
	"encoding/json"
	"testing"

	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

func TestDeleteNodeAffinityKeyFromNodes(t *testing.T) {

	testList := []struct {
		label                string
		createUserDefinedKey bool
	}{
		{
			label:                "case 1", // verify deleteNodeAffinityKeyFromNodes deletes default NodeAffinityKey only
			createUserDefinedKey: false,
		},
		{
			label:                "case 2", // verify deleteNodeAffinityKeyFromNodes does not deletes user defined NodeAffinityKey
			createUserDefinedKey: true,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &CloudPlatform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteNodeAffinityKeyFromNodes(t, reconciler, sc, obj.createUserDefinedKey)
		}
	}
}

func assertTestDeleteNodeAffinityKeyFromNodes(
	t *testing.T, reconciler ReconcileStorageCluster, sc *api.StorageCluster, createUserDefinedKey bool) {

	if createUserDefinedKey {
		addFakeNodeAffinityKeyOnNodesAndSC(t, reconciler, sc)
	}

	// verify there are eligible nodes
	nodes, err := reconciler.getStorageClusterEligibleNodes(sc, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(nodes.Items))

	if !createUserDefinedKey {
		// verify default NodeAffinityKey present on nodes
		for _, node := range nodes.Items {
			_, ok := node.ObjectMeta.Labels[defaults.NodeAffinityKey]
			assert.Equal(t, ok, true)
		}
	}

	// delete NodeAffinityKey
	err = reconciler.deleteNodeAffinityKeyFromNodes(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	nodes, err = reconciler.getStorageClusterEligibleNodes(sc, reconciler.reqLogger)
	assert.NoError(t, err)

	if !createUserDefinedKey {
		assert.Equal(t, 0, len(nodes.Items))
	} else {
		assert.NotEqual(t, 0, len(nodes.Items))
	}
}

func addFakeNodeAffinityKeyOnNodesAndSC(t *testing.T, reconciler ReconcileStorageCluster, sc *api.StorageCluster) {
	// create user defined key and val and apply it on SC
	fakeKey, fakeVal := "fakeKey", "fakeVal"
	sc.Spec.LabelSelector = metav1.AddLabelToSelector(&metav1.LabelSelector{}, fakeKey, fakeVal)

	// get all nodes
	nodes := &corev1.NodeList{}
	err := reconciler.client.List(context.TODO(), nodes)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(nodes.Items))

	// Add labels on nodes
	for _, node := range nodes.Items {
		new := node.DeepCopy()
		new.ObjectMeta.Labels[fakeKey] = fakeVal

		oldJSON, err := json.Marshal(node)
		assert.NoError(t, err)

		newJSON, err := json.Marshal(new)
		assert.NoError(t, err)

		patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
		assert.NoError(t, err)

		err = reconciler.client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		assert.NoError(t, err)
	}
}
