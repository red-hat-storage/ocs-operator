package storagecluster

import (
	"testing"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookalpha "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
)

func TestReconcilerImplInterface(t *testing.T) {
	reconciler := ReconcileStorageCluster{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestNonWatchedResourceNameNotFound(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "doesn't exist",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedResourceNamespaceNotFound(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "doesn't exist",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithNoCephClusterType(t *testing.T) {
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithTheCephClusterType(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr, &rookCephv1.CephCluster{})
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, request.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)
	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestEnsureCephClusterCreate(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "doesn't exist",
			Namespace: "storage-test-ns",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr, cephMock)
	err := reconciler.ensureCephCluster(cr, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(cr, "")
	actual := newCephCluster(cr, "")
	err = reconciler.client.Get(nil, request.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterUpdate(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cephMock)
	err := reconciler.ensureCephCluster(cr, reconciler.reqLogger)
	assert.NoError(t, err)

	expected := newCephCluster(cr, "")
	actual := newCephCluster(cr, "")
	err = reconciler.client.Get(nil, request.NamespacedName, actual)
	assert.NoError(t, err)
	assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
	assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
	assert.Equal(t, expected.Spec, actual.Spec)
}

func TestEnsureCephClusterNoConditions(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cc := newCephCluster(cr, "")
	cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key" //for test purpose
	reconciler := createFakeStorageClusterReconciler(t, cc)
	err := reconciler.ensureCephCluster(cr, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.NotEmpty(t, reconciler.conditions)
	assert.Len(t, reconciler.conditions, 3)

	expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
		conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
		conditionsv1.ConditionProgressing: corev1.ConditionTrue,
		conditionsv1.ConditionUpgradeable: corev1.ConditionFalse,
	}
	for cType, status := range expectedConditions {
		found := assertCondition(reconciler.conditions, cType, status)
		assert.True(t, found, "expected status condition not found", cType, status)
	}
}

func TestEnsureCephClusterNegativeConditions(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cc := newCephCluster(cr, "")
	cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
	cc.Status.State = rookCephv1.ClusterStateCreated
	reconciler := createFakeStorageClusterReconciler(t, cc)
	err := reconciler.ensureCephCluster(cr, reconciler.reqLogger)
	assert.NoError(t, err)
	assert.Empty(t, reconciler.conditions)
}

func TestStorageClusterCephClusterCreation(t *testing.T) {
	storageClassName := "gp2"
	volMode := corev1.PersistentVolumeBlock
	expected := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
		Spec: api.StorageClusterSpec{
			StorageDeviceSets: []api.StorageDeviceSet{
				{
					Name:      "mock-sds",
					Count:     2,
					Resources: corev1.ResourceRequirements{},
					Placement: rookalpha.Placement{},
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName,
							AccessModes:      []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
							VolumeMode:       &volMode,
						},
					},
				},
			},
		},
	}

	actual := newCephCluster(expected, "")
	assert.Equal(t, expected.Name, actual.Name)
	assert.Equal(t, expected.Namespace, actual.Namespace)
	assert.Equal(t, expected.Spec.StorageDeviceSets[0].Name, actual.Spec.Storage.StorageClassDeviceSets[0].Name)
	assert.Equal(t, expected.Spec.StorageDeviceSets[0].Count, actual.Spec.Storage.StorageClassDeviceSets[0].Count)
	// StorageCluster controller sets a default ResourceRequirements when missing for StorageClassDeviceSets
	assert.Equal(t, defaults.DaemonResources["osd"], actual.Spec.Storage.StorageClassDeviceSets[0].Resources)
	assert.Equal(t, expected.Spec.StorageDeviceSets[0].DataPVCTemplate.Spec, actual.Spec.Storage.StorageClassDeviceSets[0].VolumeClaimTemplates[0].Spec)
	// StorageCluster controller adds a default placement config for OSD StorageClassDeviceSets
	assert.Equal(t, defaults.DaemonPlacements["osd"], actual.Spec.Storage.StorageClassDeviceSets[0].Placement)
}

func TestStorageClusterInitConditions(t *testing.T) {
	cr := &api.StorageCluster{
		TypeMeta: metav1.TypeMeta{
			Kind: "StorageCluster",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	cephMock := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, cr, cephMock)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.client.Get(nil, request.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func assertExpectedCondition(t *testing.T, conditions []conditionsv1.Condition) {
	expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
		api.ConditionReconcileComplete:    corev1.ConditionUnknown,
		conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
		conditionsv1.ConditionProgressing: corev1.ConditionTrue,
		conditionsv1.ConditionDegraded:    corev1.ConditionFalse,
		conditionsv1.ConditionUpgradeable: corev1.ConditionUnknown,
	}
	for cType, status := range expectedConditions {
		found := assertCondition(conditions, cType, status)
		assert.True(t, found, "expected status condition not found", cType, status)
	}
}

func assertCondition(conditions []conditionsv1.Condition, conditionType conditionsv1.ConditionType, status corev1.ConditionStatus) bool {
	for _, objCondition := range conditions {
		if objCondition.Type == conditionType {
			if objCondition.Status == status {
				return true
			}
		}
	}
	return false
}

func createFakeStorageClusterReconciler(t *testing.T, obj ...runtime.Object) ReconcileStorageCluster {
	scheme := createFakeScheme(t, obj...)
	client := fake.NewFakeClientWithScheme(scheme, obj...)

	return ReconcileStorageCluster{
		client:    client,
		scheme:    scheme,
		reqLogger: logf.Log.WithName("controller_storagecluster_test"),
	}
}

func createFakeScheme(t *testing.T, obj ...runtime.Object) *runtime.Scheme {
	registerObjs := obj
	registerObjs = append(registerObjs)
	api.SchemeBuilder.Register(registerObjs...)
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	return scheme
}
