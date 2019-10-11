package ocsinitialization

import (
	"fmt"

	fakeSecClient "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1/fake"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	v1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	testingClient "k8s.io/client-go/testing"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"testing"
)

var successfulReconcileConditions = map[conditionsv1.ConditionType]corev1.ConditionStatus{
	conditionsv1.ConditionAvailable:   corev1.ConditionTrue,
	conditionsv1.ConditionProgressing: corev1.ConditionFalse,
	conditionsv1.ConditionDegraded:    corev1.ConditionFalse,
	conditionsv1.ConditionUpgradeable: corev1.ConditionTrue,
	v1.ConditionReconcileComplete:     corev1.ConditionTrue,
}

func TestReconcilerImplementsInterface(t *testing.T) {
	reconciler := ReconcileOCSInitialization{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestNonWatchedResourceNameNotFound(t *testing.T) {
	_, request, reconciler := getTestParams(true, t)
	request.Name = "foo"

	_, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
}

func TestNonWatchedResourceNamespaceNotFound(t *testing.T) {
	_, request, reconciler := getTestParams(true, t)
	request.Namespace = "foo"

	_, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
}

func TestResourceNotFoundCreated(t *testing.T) {
	ocs, request, reconciler := getTestParams(false, t)

	err := reconciler.client.Delete(nil, &ocs)
	assert.NoError(t, err)

	err = reconciler.client.Get(nil, request.NamespacedName, &ocs)
	assert.Error(t, err)

	_, err = reconciler.Reconcile(request)
	obj := v1.OCSInitialization{}
	err = reconciler.client.Get(nil, request.NamespacedName, &obj)
	assert.Equal(t, obj.Name, request.Name)
	assert.Equal(t, obj.Namespace, request.Namespace)
}

func TestSCCsAlreadyExist(t *testing.T) {
	ocs, request, reconciler := getTestParams(false, t)

	ocs.Status.SCCsCreated = true
	err := reconciler.client.Update(nil, &ocs)
	assert.NoError(t, err)

	_, err = reconciler.Reconcile(request)
	if err != nil {
		assert.Fail(t, fmt.Sprintf("Reconcile error encountered: %v", err))
	}
	obj := v1.OCSInitialization{}
	err = reconciler.client.Get(nil, request.NamespacedName, &obj)
	for cType, status := range successfulReconcileConditions {
		found := assertCondition(obj, cType, status)
		if !found {
			assert.Fail(t, fmt.Sprintf("Expected status condition %s %s not found", cType, status))
		}
	}

}

func TestSCCsEnsured(t *testing.T) {
	_, request, reconciler := getTestParams(false, t)

	_, err := reconciler.Reconcile(request)
	assert.NoError(t, err)

	obj := v1.OCSInitialization{}
	err = reconciler.client.Get(nil, request.NamespacedName, &obj)
	assert.NoError(t, err)
	assert.True(t, obj.Status.SCCsCreated)
}

func TestReconcileCompleteConditions(t *testing.T) {
	_, request, reconciler := getTestParams(false, t)

	_, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	obj := v1.OCSInitialization{}
	err = reconciler.client.Get(nil, request.NamespacedName, &obj)
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

func getTestParams(mockNamespace bool, t *testing.T) (v1.OCSInitialization, reconcile.Request, ReconcileOCSInitialization) {
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
	return ocs, request, getReconciler(t, &ocs)
}

func getReconciler(t *testing.T, objs ...runtime.Object) ReconcileOCSInitialization {
	registerObjs := []runtime.Object{&v1.OCSInitialization{}, &appsv1.Deployment{}}
	registerObjs = append(registerObjs)
	v1.SchemeBuilder.Register(registerObjs...)

	scheme, err := v1.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	client := fake.NewFakeClientWithScheme(scheme, objs...)
	secClient := &fakeSecClient.FakeSecurityV1{Fake: &testingClient.Fake{}}

	return ReconcileOCSInitialization{
		scheme:    scheme,
		client:    client,
		secClient: secClient,
	}
}
