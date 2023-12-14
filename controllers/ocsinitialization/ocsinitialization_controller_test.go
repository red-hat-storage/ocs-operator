package ocsinitialization

import (
	"context"
	"fmt"
	"testing"

	secv1 "github.com/openshift/api/security/v1"
	fakeSecClient "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1/fake"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testingClient "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var successfulReconcileConditions = map[conditionsv1.ConditionType]corev1.ConditionStatus{
	conditionsv1.ConditionAvailable:   corev1.ConditionTrue,
	conditionsv1.ConditionProgressing: corev1.ConditionFalse,
	conditionsv1.ConditionDegraded:    corev1.ConditionFalse,
	conditionsv1.ConditionUpgradeable: corev1.ConditionTrue,
	v1.ConditionReconcileComplete:     corev1.ConditionTrue,
}

func getTestParams(mockNamespace bool, t *testing.T) (v1.OCSInitialization, reconcile.Request, OCSInitializationReconciler) {
	var request reconcile.Request
	if mockNamespace {
		request = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      "test",
				Namespace: "test-ns",
			},
		}
	} else {
		request = reconcile.Request{NamespacedName: InitNamespacedName()}
	}
	ocs := v1.OCSInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Name,
			Namespace: request.Namespace,
		},
	}

	reconciler := getReconciler(t, &ocs)
	//The fake client stores the objects after adding a resource version to
	//them. This is a breaking change introduced in
	//https://github.com/kubernetes-sigs/controller-runtime/pull/1306.
	//Therefore we cannot use the fake object that we provided as input to the
	//the fake client and should use the object obtained from the Get
	//operation.
	_ = reconciler.Client.Get(context.TODO(), request.NamespacedName, &ocs)

	return ocs, request, reconciler
}

func getReconciler(t *testing.T, objs ...client.Object) OCSInitializationReconciler {
	ocsinit := &v1.OCSInitialization{}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).WithStatusSubresource(ocsinit).Build()
	secClient := &fakeSecClient.FakeSecurityV1{Fake: &testingClient.Fake{}}
	log := logf.Log.WithName("controller_storagecluster_test")

	return OCSInitializationReconciler{
		Scheme:         scheme,
		Client:         client,
		SecurityClient: secClient,
		Log:            log,
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	err := v1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}

	err = appsv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add appsv1 scheme")
	}

	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}

	err = monitoringv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add monitoringv1 scheme")
	}

	err = secv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add securityv1 scheme")
	}

	return scheme
}

func TestReconcilerImplementsInterface(t *testing.T) {
	reconciler := OCSInitializationReconciler{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestNonWatchedResourceNotFound(t *testing.T) {
	testcases := []struct {
		label     string
		name      string
		namespace string
	}{
		{
			label:     "Case 1", // non-watched resource (name = "foo") not found
			name:      "foo",
			namespace: "test-ns",
		},
		{
			label:     "Case 2", // non-watched resource (namespace = "foo") not found
			name:      "test",
			namespace: "foo",
		},
	}

	for _, tc := range testcases {
		_, request, reconciler := getTestParams(true, t)
		request.Name = tc.name
		request.Namespace = tc.namespace
		_, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoErrorf(t, err, "[%s]: failed to reconcile with non watched resource", tc.label)
	}
}

func TestNonWatchedResourceFound(t *testing.T) {
	testcases := []struct {
		label     string
		name      string
		namespace string
	}{
		{
			label:     "Case 1", // non-watched resource (name = "foo") already created
			name:      "foo",
			namespace: "test-ns",
		},
		{
			label:     "Case 2", // non-watched resource (namespace = "foo") already created
			name:      "test",
			namespace: "foo",
		},
	}

	for _, tc := range testcases {
		ctx := context.TODO()
		_, _, reconciler := getTestParams(true, t)
		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      tc.name,
				Namespace: tc.namespace,
			},
		}
		ocs := v1.OCSInitialization{
			ObjectMeta: metav1.ObjectMeta{
				Name:      tc.name,
				Namespace: tc.namespace,
			},
		}
		err := reconciler.Client.Create(ctx, &ocs)
		assert.NoErrorf(t, err, "[%s]: failed CREATE of non watched resource", tc.label)
		_, err = reconciler.Reconcile(context.TODO(), request)
		assert.NoErrorf(t, err, "[%s]: failed to reconcile with non watched resource", tc.label)
		actual := &v1.OCSInitialization{}
		err = reconciler.Client.Get(ctx, request.NamespacedName, actual)
		assert.NoErrorf(t, err, "[%s]: failed GET of actual resource", tc.label)
		assert.Equalf(t, statusutil.PhaseIgnored, actual.Status.Phase, "[%s]: failed to update phase of non watched resource that already exists OCS:\n%v", tc.label, actual)
	}
}

func TestCreateWatchedResource(t *testing.T) {
	testcases := []struct {
		label          string
		alreadyCreated bool
	}{
		{
			label:          "Case 1", // ocsInit resource not created already before reconcile
			alreadyCreated: false,
		},
		{
			label:          "Case 2", // ocsInit resource already created before reconcile
			alreadyCreated: true,
		},
	}

	for _, tc := range testcases {
		ctx := context.TODO()
		ocs, request, reconciler := getTestParams(false, t)
		if !tc.alreadyCreated {
			err := reconciler.Client.Delete(ctx, &ocs)
			assert.NoError(t, err)

			err = reconciler.Client.Get(ctx, request.NamespacedName, &ocs)
			assert.Error(t, err)
		}
		_, err := reconciler.Reconcile(ctx, request)
		assert.NoError(t, err)
		obj := v1.OCSInitialization{}
		_ = reconciler.Client.Get(ctx, request.NamespacedName, &obj)
		assert.Equalf(t, obj.Name, request.Name, "[%s]: failed to create ocsInit resource with correct name", tc.label)
		assert.Equalf(t, obj.Namespace, request.Namespace, "[%s]: failed to create ocsInit resource with correct namespace", tc.label)
	}
}

// TestCreateSCCs ensures that the reconciler creates the SCCs if they are missing.
func TestCreateSCCs(t *testing.T) {
	testcases := []struct {
		label      string
		sccCreated bool
	}{
		{
			label:      "Case 1", // sccs already created before reconcile
			sccCreated: true,
		},
		{
			label:      "Case 2",
			sccCreated: false, // sccs not created before reconcile
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)

		if tc.sccCreated {
			ocs.Status.SCCsCreated = true
			err := reconciler.Client.Status().Update(context.TODO(), &ocs)
			assert.NoErrorf(t, err, "[%s]: failed to update ocsInit status", tc.label)
		}

		obj := v1.OCSInitialization{}
		err := reconciler.Client.Get(context.TODO(), request.NamespacedName, &obj)
		assert.NoErrorf(t, err, "[%s]: failed to get ocsInit", tc.label)
		assert.Equal(t, tc.sccCreated, obj.Status.SCCsCreated, "[%s] failed to set the pre condition for the test", tc.label)

		_, err = reconciler.Reconcile(context.TODO(), request)
		assert.NoErrorf(t, err, "[%s]: failed to reconcile ocsInit", tc.label)
		_ = reconciler.Client.Get(context.TODO(), request.NamespacedName, &obj)
		for cType, status := range successfulReconcileConditions {
			found := assertCondition(obj, cType, status)
			if !found {
				assert.Fail(t, fmt.Sprintf("Expected status condition %s %s not found", cType, status))
			}
		}
		assert.Equal(t, true, obj.Status.SCCsCreated, "[%s] SCCs not created after reconcile", tc.label)
	}
}

func TestReconcileCompleteConditions(t *testing.T) {
	_, request, reconciler := getTestParams(false, t)

	_, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	obj := v1.OCSInitialization{}
	_ = reconciler.Client.Get(context.TODO(), request.NamespacedName, &obj)
	assert.NotEmpty(t, obj.Status.Conditions)
	assert.Len(t, obj.Status.Conditions, 5)
	for cType, status := range successfulReconcileConditions {
		found := assertCondition(obj, cType, status)
		if !found {
			assert.Fail(t, "expected status condition not found")
		}
	}
}

func assertCondition(ocs v1.OCSInitialization, conditionType conditionsv1.ConditionType, status corev1.ConditionStatus) bool {
	for _, objCondition := range ocs.Status.Conditions {
		if objCondition.Type == conditionType {
			if objCondition.Status == status {
				return true
			}
		}
	}
	return false
}
