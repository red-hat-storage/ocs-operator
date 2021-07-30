package storagecluster

import (
	"context"
	"encoding/json"
	"testing"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v4/apis/volumesnapshot/v1"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	api "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestReconcileUninstallAnnotations(t *testing.T) {
	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}
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
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster,
	CleanupPolicy CleanupPolicyType, UninstallMode UninstallModeType) {

	err := reconciler.reconcileUninstallAnnotations(sc)
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
		cp := &Platform{platform: eachPlatform}
		t, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

		// there are two annotations which will be 4 combinations, test all 4 combinations

		// set default uninstall annotations
		err := reconciler.reconcileUninstallAnnotations(sc)
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
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster,
	CleanupPolicyConfirmation cephv1.CleanupConfirmationProperty, AllowUninstallWithVolumes bool) {

	// verify it set the cleanup policy and uninstall mode on cephCluster wrt annotations
	err := reconciler.setRookUninstallandCleanupPolicy(sc)
	assert.NoError(t, err)

	cephCluster := &cephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	assert.NoError(t, err)

	assert.Equal(t, CleanupPolicyConfirmation, cephCluster.Spec.CleanupPolicy.Confirmation)
	assert.Equal(t, AllowUninstallWithVolumes, cephCluster.Spec.CleanupPolicy.AllowUninstallWithVolumes)
}

func TestDeleteStorageClasses(t *testing.T) {

	testList := []struct {
		label              string
		storageClassExists bool
	}{
		{
			label:              "case 1", // verify storage classes are present and delete them
			storageClassExists: true,
		},
		{
			label:              "case 2", // verify storage classes does not exist and delete should not get error out
			storageClassExists: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			t, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)
			assertTestDeleteStorageClasses(t, reconciler, sc, obj.storageClassExists)
		}
	}
}

func assertTestDeleteStorageClasses(t *testing.T, reconciler StorageClusterReconciler,
	sc *api.StorageCluster, storageClassExists bool) {

	var obj ocsStorageClass
	if !storageClassExists {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	sccs, err := reconciler.newStorageClassConfigurations(sc)
	assert.NoError(t, err)

	for _, scc := range sccs {
		existing := storagev1.StorageClass{}
		err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: scc.storageClass.Name}, &existing)
		assert.Equal(t, !storageClassExists, errors.IsNotFound(err))
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	for _, scc := range sccs {
		existing := storagev1.StorageClass{}
		err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: scc.storageClass.Name}, &existing)
		assert.True(t, errors.IsNotFound(err))
	}
}

func TestDeleteSnapshotClasses(t *testing.T) {

	testList := []struct {
		label               string
		SnapshotClassExists bool
	}{
		{
			label:               "case 1", // verify SnapshotClass are present and delete them
			SnapshotClassExists: true,
		},
		{
			label:               "case 2", // verify SnapshotClass does not exist and delete should not get error out
			SnapshotClassExists: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			t, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)
			assertTestDeleteStorageClasses(t, reconciler, sc, obj.SnapshotClassExists)
		}
	}
}

//nolint // func assertTestDeleteSnapshotClasses is not used. For Future usuage func is created.
func assertTestDeleteSnapshotClasses(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, SnapshotClassExists bool) {

	var obj ocsSnapshotClass

	if !SnapshotClassExists {
		err := obj.ensureCreated(&reconciler, sc)
		assert.NoError(t, err)
	}

	vsscs := newSnapshotClassConfigurations(sc)

	for _, vssc := range vsscs {
		existing := snapapi.VolumeSnapshotClass{}
		err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: vssc.snapshotClass.Name}, &existing)
		assert.Equal(t, !SnapshotClassExists, errors.IsNotFound(err))
	}

	err := obj.ensureCreated(&reconciler, sc)
	assert.NoError(t, err)

	for _, vssc := range vsscs {
		existing := snapapi.VolumeSnapshotClass{}
		err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: vssc.snapshotClass.Name}, &existing)
		assert.Equal(t, !SnapshotClassExists, errors.IsNotFound(err))
		assert.True(t, errors.IsNotFound(err))
	}
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
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteNodeAffinityKeyFromNodes(t, reconciler, sc, obj.createUserDefinedKey)
		}
	}
}

func assertTestDeleteNodeAffinityKeyFromNodes(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, createUserDefinedKey bool) {

	if createUserDefinedKey {
		addFakeNodeAffinityKeyOnNodesAndSC(t, reconciler, sc)
	}

	// verify there are eligible nodes
	nodes, err := reconciler.getStorageClusterEligibleNodes(sc)
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
	err = reconciler.deleteNodeAffinityKeyFromNodes(sc)
	assert.NoError(t, err)

	nodes, err = reconciler.getStorageClusterEligibleNodes(sc)
	assert.NoError(t, err)

	if !createUserDefinedKey {
		assert.Equal(t, 0, len(nodes.Items))
	} else {
		assert.NotEqual(t, 0, len(nodes.Items))
	}
}

func addFakeNodeAffinityKeyOnNodesAndSC(t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster) {
	// create user defined key and val and apply it on SC
	fakeKey, fakeVal := "fakeKey", "fakeVal"
	sc.Spec.LabelSelector = metav1.AddLabelToSelector(&metav1.LabelSelector{}, fakeKey, fakeVal)

	// get all nodes
	nodes := &corev1.NodeList{}
	err := reconciler.Client.List(context.TODO(), nodes)
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

		err = reconciler.Client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		assert.NoError(t, err)
	}
}

func TestDeleteNodeTaint(t *testing.T) {
	testList := []struct {
		label                  string
		createDefaultNodeTaint bool
	}{
		{
			label:                  "case 1", // verify deleteNodeTaint deletes the default NodeTaint
			createDefaultNodeTaint: true,
		},
		{
			label:                  "case 2", // verify does not get error out when default node taints does not exist
			createDefaultNodeTaint: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteNodeTaint(t, reconciler, sc, obj.createDefaultNodeTaint)
		}
	}
}

func assertTestDeleteNodeTaint(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, createDefaultNodeTaint bool) {

	if createDefaultNodeTaint {
		addDefaultNodeTaintOnNodes(t, reconciler, sc)
	}

	// delete node taints
	err := reconciler.deleteNodeTaint(sc)
	assert.NoError(t, err)

	nodes, err := reconciler.getStorageClusterEligibleNodes(sc)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(nodes.Items))

	// verify deleteNodeTaint deleted the default node taint
	for _, node := range nodes.Items {
		for _, taint := range node.Spec.Taints {
			assert.NotEqual(t, defaults.NodeTolerationKey, taint.Key)
		}
	}
}

func addDefaultNodeTaintOnNodes(t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster) {

	nodes, err := reconciler.getStorageClusterEligibleNodes(sc)
	assert.NoError(t, err)
	assert.NotEqual(t, 0, len(nodes.Items))

	for _, node := range nodes.Items {
		new := node.DeepCopy()
		new.Spec.Taints = append(new.Spec.Taints, corev1.Taint{Key: defaults.NodeTolerationKey, Effect: corev1.TaintEffectNoSchedule})

		oldJSON, err := json.Marshal(node)
		assert.NoError(t, err)

		newJSON, err := json.Marshal(new)
		assert.NoError(t, err)

		patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, node)
		assert.NoError(t, err)

		err = reconciler.Client.Patch(context.TODO(), &node, client.RawPatch(types.StrategicMergePatchType, patch))
		assert.NoError(t, err)
	}
}

func TestDeleteCephCluster(t *testing.T) {

	testList := []struct {
		label            string
		cephClusterExist bool
	}{
		{
			label:            "case 1", // verify deleteCephCluster deletes the CephCluster
			cephClusterExist: true,
		},
		{
			label:            "case 2", // verify does not get error out when CephCluster does not exist
			cephClusterExist: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteCephCluster(t, reconciler, sc, obj.cephClusterExist)
		}
	}
}

func assertTestDeleteCephCluster(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, cephClusterExist bool) {

	var obj ocsCephCluster

	if !cephClusterExist {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	cephCluster := &cephv1.CephCluster{}
	err := reconciler.Client.Get(context.TODO(), types.NamespacedName{
		Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)

	if cephClusterExist {
		assert.NoError(t, err)
	} else {
		assert.True(t, errors.IsNotFound(err))
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	cephCluster = &cephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
		Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	assert.True(t, errors.IsNotFound(err))
}

func TestDeleteCephFilesystems(t *testing.T) {

	testList := []struct {
		label                string
		cephFilesystemsExist bool
	}{
		{
			label:                "case 1", // verify deleteCephFilesystems deletes the CephFilesystem
			cephFilesystemsExist: true,
		},
		{
			label:                "case 2", // verify does not get error out when CephFilesystem does not exist
			cephFilesystemsExist: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteCephFilesystems(t, reconciler, sc, obj.cephFilesystemsExist)
		}
	}
}

func assertTestDeleteCephFilesystems(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, cephFilesystemsExist bool) {

	var obj ocsCephFilesystems

	if !cephFilesystemsExist {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	cephFilesystems, err := reconciler.newCephFilesystemInstances(sc)
	assert.NoError(t, err)

	for _, cephFilesystem := range cephFilesystems {
		foundCephFilesystem := &cephv1.CephFilesystem{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)

		if cephFilesystemsExist {
			assert.NoError(t, err)
		} else {
			assert.True(t, errors.IsNotFound(err))
		}
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	for _, cephFilesystem := range cephFilesystems {
		foundCephFilesystem := &cephv1.CephFilesystem{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephFilesystem.Name, Namespace: sc.Namespace}, foundCephFilesystem)
		assert.True(t, errors.IsNotFound(err))
	}
}

func TestDeleteCephBlockPools(t *testing.T) {

	testList := []struct {
		label               string
		cephBlockPoolsExist bool
	}{
		{
			label:               "case 1", // verify deleteCephBlockPools deletes the CephBlockPools
			cephBlockPoolsExist: true,
		},
		{
			label:               "case 2", // verify does not get error out when CephBlockPools does not exist
			cephBlockPoolsExist: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, nil)

			assertTestDeleteCephFilesystems(t, reconciler, sc, obj.cephBlockPoolsExist)
		}
	}
}

//nolint // func assertTestDeleteCephBlockPools is not used. For Future usuage func is created.
func assertTestDeleteCephBlockPools(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, cephBlockPoolsExist bool) {

	var obj ocsCephBlockPools

	if !cephBlockPoolsExist {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	cephBlockPools, err := reconciler.newCephBlockPoolInstances(sc)
	assert.NoError(t, err)

	for _, cephBlockPool := range cephBlockPools {
		foundCephBlockPool := &cephv1.CephBlockPool{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)

		if cephBlockPoolsExist {
			assert.NoError(t, err)
		} else {
			assert.True(t, errors.IsNotFound(err))
		}
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	for _, cephBlockPool := range cephBlockPools {
		foundCephBlockPool := &cephv1.CephBlockPool{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)
		assert.True(t, errors.IsNotFound(err))
	}
}

func getFakeCephObjectStoreUser() *cephv1.CephObjectStoreUser {

	sc := createDefaultStorageCluster()

	return &cephv1.CephObjectStoreUser{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephObjectStoreUser(sc),
			Namespace: sc.Namespace,
		},
		Spec: cephv1.ObjectStoreUserSpec{
			DisplayName: sc.Name,
			Store:       generateNameForCephObjectStore(sc),
		},
	}
}

func TestDeleteCephObjectStoreUsers(t *testing.T) {

	testList := []struct {
		label                     string
		CephObjectStoreUsersExist bool
	}{
		{
			label:                     "case 1", // verify deleteCephObjectStoreUsers deletes the CephObjectStoreUsers
			CephObjectStoreUsersExist: true,
		},
		{
			label:                     "case 2", // verify does not get error out when CephObjectStoreUsers does not exist
			CephObjectStoreUsersExist: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			fakeCephObjectStoreUser := getFakeCephObjectStoreUser()
			runtimeObjs := []client.Object{fakeCephObjectStoreUser}
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, runtimeObjs)

			assertTestDeleteCephObjectStoreUsers(t, reconciler, sc, obj.CephObjectStoreUsersExist)
		}
	}
}

func assertTestDeleteCephObjectStoreUsers(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, CephObjectStoreUsersExist bool) {

	var obj ocsCephObjectStoreUsers

	if !CephObjectStoreUsersExist {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	cephStoreUsers, err := reconciler.newCephObjectStoreUserInstances(sc)
	assert.NoError(t, err)

	for _, cephStoreUser := range cephStoreUsers {
		foundCephStoreUser := &cephv1.CephObjectStoreUser{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephStoreUser.Name, Namespace: sc.Namespace}, foundCephStoreUser)

		if CephObjectStoreUsersExist {
			assert.NoError(t, err)
		} else {
			assert.True(t, errors.IsNotFound(err))
		}
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	for _, cephStoreUser := range cephStoreUsers {
		foundCephStoreUser := &cephv1.CephObjectStoreUser{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephStoreUser.Name, Namespace: sc.Namespace}, foundCephStoreUser)

		assert.True(t, errors.IsNotFound(err))
	}
}

func getFakeCephObjectStore() *cephv1.CephObjectStore {

	sc := createDefaultStorageCluster()

	return &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephObjectStore(sc),
			Namespace: sc.Namespace,
		},
		Spec: cephv1.ObjectStoreSpec{
			PreservePoolsOnDelete: false,
			DataPool: cephv1.PoolSpec{
				FailureDomain: sc.Status.FailureDomain,
				Replicated: cephv1.ReplicatedSpec{
					Size: 3,
				},
			},
			MetadataPool: cephv1.PoolSpec{
				FailureDomain: sc.Status.FailureDomain,
				Replicated: cephv1.ReplicatedSpec{
					Size: 3,
				},
			},
			Gateway: cephv1.GatewaySpec{
				Port:      80,
				Instances: 2,
				Placement: defaults.DaemonPlacements["rgw"],
				Resources: defaults.GetDaemonResources("rgw", sc.Spec.Resources),
			},
		},
	}
}

func TestDeleteCephObjectStores(t *testing.T) {

	testList := []struct {
		label                string
		CephObjectStoreExist bool
	}{
		{
			label:                "case 1", // verify deleteCephObjectStore deletes the CephObjectStore
			CephObjectStoreExist: true,
		},
		{
			label:                "case 2", // verify does not get error out when CephObjectStore does not exist
			CephObjectStoreExist: false,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			fakeCephObjectStore := getFakeCephObjectStore()
			runtimeObjs := []client.Object{fakeCephObjectStore}
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, runtimeObjs)

			assertTestDeleteCephObjectStores(t, reconciler, sc, obj.CephObjectStoreExist)
		}
	}
}

func assertTestDeleteCephObjectStores(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster, CephObjectStoreExist bool) {

	var obj ocsCephObjectStores

	if !CephObjectStoreExist {
		err := obj.ensureDeleted(&reconciler, sc)
		assert.NoError(t, err)
	}

	cephStores, err := reconciler.newCephObjectStoreInstances(sc)
	assert.NoError(t, err)

	for _, cephStore := range cephStores {
		foundCephStore := &cephv1.CephObjectStore{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephStore.Name, Namespace: sc.Namespace}, foundCephStore)

		if CephObjectStoreExist {
			assert.NoError(t, err)
		} else {
			assert.True(t, errors.IsNotFound(err))
		}
	}

	err = obj.ensureDeleted(&reconciler, sc)
	assert.NoError(t, err)

	for _, cephStore := range cephStores {
		foundCephStore := &cephv1.CephObjectStore{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{
			Name: cephStore.Name, Namespace: sc.Namespace}, foundCephStore)

		assert.True(t, errors.IsNotFound(err))
	}
}

func getFakeNoobaa() *nbv1.NooBaa {

	sc := createDefaultStorageCluster()

	return &nbv1.NooBaa{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NooBaa",
			APIVersion: "noobaa.io/v1alpha1'",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: sc.Namespace,
		},
	}
}

func TestSetNoobaaUninstallMode(t *testing.T) {

	testList := []struct {
		label               string
		UninstallMode       UninstallModeType
		NoobaaUninstallMode nbv1.CleanupConfirmationProperty
	}{
		{
			label:               "case 1", // verify setNoobaaUninstallMode set NooBaa cleanup policy to ""
			UninstallMode:       UninstallModeGraceful,
			NoobaaUninstallMode: "",
		},
		{
			label:               "case 2", // verify setNoobaaUninstallMode set NooBaa cleanup policy to nbv1.DeleteOBCConfirmation
			UninstallMode:       UninstallModeForced,
			NoobaaUninstallMode: nbv1.DeleteOBCConfirmation,
		},
	}

	for _, eachPlatform := range allPlatforms {
		cp := &Platform{platform: eachPlatform}

		for _, obj := range testList {
			fakeNoobaa := getFakeNoobaa()
			runtimeObjs := []client.Object{fakeNoobaa}
			_, reconciler, sc, _ := initStorageClusterResourceCreateUpdateTestWithPlatform(t, cp, runtimeObjs)

			assertTestSetNoobaaUninstallMode(t, reconciler, sc, obj.UninstallMode, obj.NoobaaUninstallMode)
		}
	}
}

func assertTestSetNoobaaUninstallMode(
	t *testing.T, reconciler StorageClusterReconciler, sc *api.StorageCluster,
	UninstallMode UninstallModeType, NoobaaUninstallMode nbv1.CleanupConfirmationProperty) {

	err := reconciler.reconcileUninstallAnnotations(sc)
	assert.NoError(t, err)

	sc.ObjectMeta.Annotations[UninstallModeAnnotation] = string(UninstallMode)

	err = reconciler.setNoobaaUninstallMode(sc)
	assert.NoError(t, err)

	noobaa := &nbv1.NooBaa{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	assert.NoError(t, err)

	assert.Equal(t, NoobaaUninstallMode, noobaa.Spec.CleanupPolicy.Confirmation)
}
