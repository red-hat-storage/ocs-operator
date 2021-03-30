package ocsinitialization

import (
	"context"
	"fmt"
	"testing"

	secv1 "github.com/openshift/api/security/v1"
	fakeSecClient "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1/fake"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	v1 "github.com/openshift/ocs-operator/api/v1"
	statusutil "github.com/openshift/ocs-operator/controllers/util"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
	ocsRecon := v1.OCSInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      request.Name,
			Namespace: request.Namespace,
		},
	}

	return ocs, request, getReconciler(t, &ocsRecon)
}

func getReconciler(t *testing.T, objs ...client.Object) OCSInitializationReconciler {
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithObjects(objs...).Build()
	secClient := &fakeSecClient.FakeSecurityV1{Fake: &testingClient.Fake{}}
	log := logf.Log.WithName("controller_storagecluster_test")

	return OCSInitializationReconciler{
		Scheme:         scheme,
		Client:         client,
		SecurityClient: secClient,
		Log:            log,
		RookImage:      "rook/ceph",
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := v1.SchemeBuilder.Build()
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

//nolint //ignoring errcheck causing the failures
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
		ocs, request, reconciler := getTestParams(true, t)
		request.Name = tc.name
		request.Namespace = tc.namespace
		ocs.Name = tc.name
		ocs.Namespace = tc.namespace
		reconciler.Client.Create(nil, &ocs)
		_, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoErrorf(t, err, "[%s]: failed to reconcile with non watched resource", tc.label)
		actual := &v1.OCSInitialization{}
		reconciler.Client.Get(nil, request.NamespacedName, actual)
		assert.Equalf(t, statusutil.PhaseIgnored, actual.Status.Phase, "[%s]: failed to update phase of non watched resource that already exists", tc.label)
	}
}

//nolint //ignoring errcheck as causing failures
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
		ocs, request, reconciler := getTestParams(false, t)
		if tc.alreadyCreated {
			reconciler.Client.Create(nil, &ocs)
		} else {
			err := reconciler.Client.Delete(nil, &ocs)
			assert.NoError(t, err)

			err = reconciler.Client.Get(nil, request.NamespacedName, &ocs)
			assert.Error(t, err)
		}
		_, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoError(t, err)
		obj := v1.OCSInitialization{}
		_ = reconciler.Client.Get(nil, request.NamespacedName, &obj)
		assert.Equalf(t, obj.Name, request.Name, "[%s]: failed to create ocsInit resource with correct name", tc.label)
		assert.Equalf(t, obj.Namespace, request.Namespace, "[%s]: failed to create ocsInit resource with correct namespace", tc.label)
	}
}

func TestCreateSCCs(t *testing.T) {
	testcases := []struct {
		label      string
		sscCreated bool
	}{
		{
			label:      "Case 1", // sscs already created before reconcile
			sscCreated: true,
		},
		{
			label:      "Case 2",
			sscCreated: false, // sscs not created before reconcile
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)

		if tc.sscCreated {
			ocs.Status.SCCsCreated = false
			// TODO: uncomment this!
			//err := reconciler.Client.Update(context.TODO(), &ocs)
			//assert.NoErrorf(t, err, "[%s]: failed to update ocsInit status", tc.label)
		}

		_, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoErrorf(t, err, "[%s]: failed to reconcile ocsInit", tc.label)
		obj := v1.OCSInitialization{}
		_ = reconciler.Client.Get(context.TODO(), request.NamespacedName, &obj)
		for cType, status := range successfulReconcileConditions {
			found := assertCondition(obj, cType, status)
			if !found {
				assert.Fail(t, fmt.Sprintf("Expected status condition %s %s not found", cType, status))
			}
		}
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

func TestEnsureToolsDeployment(t *testing.T) {
	testcases := []struct {
		label           string
		enableCephTools bool
	}{
		{
			label:           "Case 1",
			enableCephTools: true,
		},
		{
			label:           "Case 2",
			enableCephTools: false,
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools

		err := reconciler.ensureToolsDeployment(&ocs)
		assert.NoErrorf(t, err, "[%s] failed to create ceph tools", tc.label)
		if tc.enableCephTools {
			cephtoolsDeployment := &appsv1.Deployment{}
			err := reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: request.Namespace}, cephtoolsDeployment)
			assert.NoErrorf(t, err, "[%s] failed to create ceph tools", tc.label)
		}
	}
}

func TestEnsureToolsDeploymentUpdate(t *testing.T) {
	var replicaTwo int32 = 2

	testcases := []struct {
		label           string
		enableCephTools bool
	}{
		{
			label:           "Case 1", // existing ceph tools pod should get updated
			enableCephTools: true,
		},
		{
			label:           "Case 2", // existing ceph tool pod should get deleted
			enableCephTools: false,
		},
	}

	for _, tc := range testcases {
		ocs, request, reconciler := getTestParams(false, t)
		ocs.Spec.EnableCephTools = tc.enableCephTools
		cephTools := &appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      rookCephToolDeploymentName,
				Namespace: request.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Replicas: &replicaTwo,
			},
		}
		err := reconciler.Client.Create(context.TODO(), cephTools)
		assert.NoError(t, err)
		err = reconciler.ensureToolsDeployment(&ocs)
		assert.NoErrorf(t, err, "[%s] failed to create ceph tools deployment", tc.label)

		cephtoolsDeployment := &appsv1.Deployment{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: request.Namespace}, cephtoolsDeployment)
		if tc.enableCephTools {
			assert.NoErrorf(t, err, "[%s] failed to get ceph tools deployment", tc.label)
			assert.Equalf(t, int32(1), *cephtoolsDeployment.Spec.Replicas, "[%s] failed to update the ceph tools pod", tc.label)
		} else {
			assert.Errorf(t, err, "[%s] failed to delete ceph tools deployment when it was disabled in the spec", tc.label)
		}
	}
}

func TestEnsureRookCephOperatorConfig(t *testing.T) {
	ocsinit := &v1.OCSInitialization{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocsinit",
			Namespace: "openshift-storage",
		},
	}
	ocsinitFalse := ocsinit
	ocsinitFalse.Status.RookCephOperatorConfigCreated = false
	ocsinitTrue := ocsinit
	ocsinitTrue.Status.RookCephOperatorConfigCreated = true

	type args struct {
		reconciler  OCSInitializationReconciler
		initialData *v1.OCSInitialization
	}
	testcases := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "RookCephOperatorConfigCreated not set",
			args: args{
				initialData: ocsinit,
				reconciler:  getReconciler(t, ocsinit),
			},
			wantErr: false,
		},
		{
			name: "RookCephOperatorConfigCreated is false",
			args: args{
				initialData: ocsinitFalse,
				reconciler:  getReconciler(t, ocsinitFalse),
			},
			wantErr: false,
		},
		{
			name: "RookCephOperatorConfigCreated is true",
			args: args{
				initialData: ocsinitTrue,
				reconciler:  getReconciler(t, ocsinitTrue),
			},
			wantErr: false,
		},
	}
	for _, tc := range testcases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.args.reconciler.ensureRookCephOperatorConfig(tc.args.initialData); (err != nil) != tc.wantErr {
				t.Errorf("ReconcileOCSInitialization.ensureRookCephOperatorConfig() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
