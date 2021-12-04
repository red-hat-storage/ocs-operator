package storagecluster

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	coreEnvVar                 = "NOOBAA_CORE_IMAGE"
	dbEnvVar                   = "NOOBAA_DB_IMAGE"
	defaultStorageClass        = "noobaa-ceph-rbd"
	noobaDbDefaultStorageClass = "test-storage-class"
)

var noobaaReconcileTestLogger = logf.Log.WithName("noobaa_system_reconciler_test")

func TestEnsureNooBaaSystem(t *testing.T) {
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: "test_ns",
	}
	sc := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
		},
		Status: v1.StorageClusterStatus{
			Images: v1.ImagesStatus{
				NooBaaCore: &v1.ComponentImageStatus{},
				NooBaaDB:   &v1.ComponentImageStatus{},
			},
		},
	}
	noobaa := v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: namespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}

	cephCluster := cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephClusterFromString(namespacedName.Name),
			Namespace: namespacedName.Namespace,
		},
	}
	cephCluster.Status.State = cephv1.ClusterStateCreated

	addressableStorageClass := defaultStorageClass

	cases := []struct {
		label          string
		namespacedName types.NamespacedName
		sc             v1.StorageCluster
		noobaa         v1alpha1.NooBaa
		isCreate       bool
	}{
		{
			label:          "case 1", //ensure create logic
			namespacedName: namespacedName,
			sc:             *sc.DeepCopy(),
			noobaa:         *noobaa.DeepCopy(),
			isCreate:       true,
		},
		{
			label:          "case 2", //ensure update logic
			namespacedName: namespacedName,
			sc:             *sc.DeepCopy(),
			noobaa:         *noobaa.DeepCopy(),
		},
		{
			label:          "case 3", //equal, no update
			namespacedName: namespacedName,
			sc:             *sc.DeepCopy(),
			noobaa: v1alpha1.NooBaa{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
					SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
				},
				Spec: v1alpha1.NooBaaSpec{
					DBStorageClass:            &addressableStorageClass,
					PVPoolDefaultStorageClass: &addressableStorageClass,
				},
			},
		},
	}

	var obj ocsNoobaaSystem

	for _, c := range cases {
		reconciler := getReconciler(t, &v1alpha1.NooBaa{})
		reconciler.Log = noobaaReconcileTestLogger
		reconciler.Client.Create(context.TODO(), &cephCluster) //nolint //ignoring err check as causes failure

		if c.isCreate {
			err := reconciler.Client.Get(context.TODO(), namespacedName, &c.noobaa)
			assert.True(t, errors.IsNotFound(err))
		} else {
			err := reconciler.Client.Create(context.TODO(), &c.noobaa)
			assert.NoError(t, err)
		}
		err := obj.ensureCreated(&reconciler, &sc)
		assert.NoError(t, err)

		_ = reconciler.Client.Get(context.TODO(), namespacedName, &noobaa)
		assert.Equal(t, noobaa.Name, namespacedName.Name)
		assert.Equal(t, noobaa.Namespace, namespacedName.Namespace)
		if !c.isCreate {
			assert.Equal(t, *noobaa.Spec.DBStorageClass, defaultStorageClass)
			assert.Equal(t, *noobaa.Spec.PVPoolDefaultStorageClass, defaultStorageClass)
		}
	}
}

func TestNooBaaReconcileStrategy(t *testing.T) {
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: "test_ns",
	}

	cases := []struct {
		label          string
		namespacedName types.NamespacedName
		sc             v1.StorageCluster
		isCreate       bool
	}{
		{
			label: "case 1", //ensure default create logic
			sc: v1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
			},
			isCreate: true,
		},
		{
			label: "case 2", //ensure create logic
			sc: v1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
				Spec: v1.StorageClusterSpec{
					MultiCloudGateway: &v1.MultiCloudGatewaySpec{
						ReconcileStrategy: "manage",
					},
				},
			},
			isCreate: true,
		},
		{
			label: "case 3", //ensure unknown value logic
			sc: v1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
				Spec: v1.StorageClusterSpec{
					MultiCloudGateway: &v1.MultiCloudGatewaySpec{
						ReconcileStrategy: "foo",
					},
				},
			},
			isCreate: true,
		},
		{
			label: "case 4", //ensure ignore logic
			sc: v1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      namespacedName.Name,
					Namespace: namespacedName.Namespace,
				},
				Spec: v1.StorageClusterSpec{
					MultiCloudGateway: &v1.MultiCloudGatewaySpec{
						ReconcileStrategy: "ignore",
					},
				},
			},
			isCreate: false,
		},
	}

	var obj ocsNoobaaSystem

	for _, c := range cases {
		c.sc.Status.Images.NooBaaCore = &v1.ComponentImageStatus{}
		c.sc.Status.Images.NooBaaDB = &v1.ComponentImageStatus{}

		reconciler := getReconciler(t, &v1alpha1.NooBaa{})
		reconciler.Log = noobaaReconcileTestLogger

		cephCluster := cephv1.CephCluster{}
		cephCluster.Name = generateNameForCephClusterFromString(namespacedName.Name)
		cephCluster.Namespace = namespacedName.Namespace
		cephCluster.Status.State = cephv1.ClusterStateCreated
		err := reconciler.Client.Create(context.TODO(), &cephCluster)
		assert.NoError(t, err)

		err = obj.ensureCreated(&reconciler, &c.sc)
		assert.NoError(t, err)

		err = obj.ensureCreated(&reconciler, &c.sc)
		assert.NoError(t, err)

		noobaa := v1alpha1.NooBaa{}
		err = reconciler.Client.Get(context.TODO(), namespacedName, &noobaa)
		if c.isCreate {
			assert.NoError(t, err)
		} else {
			assert.True(t, errors.IsNotFound(err))
		}
	}
}

func TestSetNooBaaDesiredState(t *testing.T) {
	defaultInput := v1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test_name",
		},
	}
	cases := []struct {
		label              string
		envCore            string
		envDB              string
		sc                 v1.StorageCluster
		dbStorageClassName string
	}{
		{
			label:   "case 1", // both envVars carry through to created NooBaaSystem
			envCore: "FOO",
			envDB:   "BAR",
			sc:      defaultInput,
		},
		{
			label: "case 2", // missing core envVar causes no issue
			envDB: "BAR",
			sc:    defaultInput,
		},
		{
			label:   "case 3", // missing db envVar causes no issue
			envCore: "FOO",
			sc:      defaultInput,
		},
		{
			label: "case 4", // neither envVar set, no issues occur
			sc:    defaultInput,
		},
		{
			label: "case 5", // missing initData namespace does not cause error
			sc:    v1.StorageCluster{},
		},
		{
			label: "case 6", // dbStorageClassName should reflect DBStorageClass in nooba spec
			sc: v1.StorageCluster{
				Spec: v1.StorageClusterSpec{
					MultiCloudGateway: &v1.MultiCloudGatewaySpec{
						DbStorageClassName: noobaDbDefaultStorageClass,
					},
				},
			},
			dbStorageClassName: noobaDbDefaultStorageClass,
		},
	}

	for _, c := range cases {

		err := os.Setenv(coreEnvVar, c.envCore)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, coreEnvVar)
		}
		err = os.Setenv(dbEnvVar, c.envDB)
		if err != nil {
			assert.Failf(t, "[%s] unable to set env_var %s", c.label, dbEnvVar)
		}

		reconciler := StorageClusterReconciler{
			OperatorCondition: newStubOperatorCondition(),
			Log:               logf.Log.WithName("controller_storagecluster_test"),
		}
		_ = reconciler.initializeImageVars()

		noobaa := v1alpha1.NooBaa{
			TypeMeta: metav1.TypeMeta{
				Kind:       "NooBaa",
				APIVersion: "noobaa.io/v1alpha1'",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:      "noobaa",
				Namespace: defaultInput.Namespace,
			},
		}
		err = reconciler.setNooBaaDesiredState(&noobaa, &c.sc)
		if err != nil {
			assert.Failf(t, "[%s] unable to set noobaa desired state", c.label)
		}
		if c.dbStorageClassName == "" {
			c.dbStorageClassName = generateNameForCephBlockPoolSC(&c.sc)
		}
		assert.Equalf(t, noobaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.NotEmptyf(t, noobaa.Labels, "[%s] expected noobaa Labels not found", c.label)
		assert.Equalf(t, noobaa.Labels["app"], "noobaa", "[%s] expected noobaa Label mismatch", c.label)
		assert.Equalf(t, noobaa.Name, "noobaa", "[%s] noobaa name not set correctly", c.label)
		assert.Equal(t, *noobaa.Spec.DBStorageClass, c.dbStorageClassName)
		assert.Equal(t, *noobaa.Spec.PVPoolDefaultStorageClass, c.dbStorageClassName)
		noobaaplacement := getPlacement(&c.sc, "noobaa-core")
		assert.Equal(t, noobaa.Spec.Tolerations, noobaaplacement.Tolerations)
		assert.Equal(t, noobaa.Spec.Affinity, &corev1.Affinity{NodeAffinity: noobaaplacement.NodeAffinity})
		assert.Equalf(t, noobaa.Namespace, c.sc.Namespace, "[%s] namespace mismatch", c.label)
		if c.envCore != "" {
			assert.Equalf(t, *noobaa.Spec.Image, c.envCore, "[%s] core envVar not applied to noobaa spec", c.label)
		}
		if c.envDB != "" {
			assert.Equalf(t, *noobaa.Spec.DBImage, c.envDB, "[%s] db envVar not applied to noobaa spec", c.label)
		}
	}
}

func TestNoobaaSystemInExternalClusterMode(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createExternalClusterReconciler(t)
	result, err := reconciler.Reconcile(context.TODO(), request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertNoobaaResource(t, reconciler)
}

func assertNoobaaResource(t *testing.T, reconciler StorageClusterReconciler) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	var obj ocsNoobaaSystem

	cr := &v1.StorageCluster{}
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, cr)
	assert.NoError(t, err)

	// get the ceph cluster
	request.Name = generateNameForCephCluster(cr)
	foundCeph := &cephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, foundCeph)
	assert.NoError(t, err)

	// set the state to 'ClusterStateConnecting' (to mock a state where external cluster is still trying to connect)
	foundCeph.Status.State = cephv1.ClusterStateConnecting
	err = reconciler.Client.Update(context.TODO(), foundCeph)
	assert.NoError(t, err)
	// calling 'ensureNoobaaSystem()' function and the expectation is that 'Noobaa' system is not be created
	err = obj.ensureCreated(&reconciler, cr)
	assert.NoError(t, err)
	fNoobaa := &v1alpha1.NooBaa{}
	request.Name = "noobaa"
	// expectation is not to get any Noobaa object
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, fNoobaa)
	assert.Error(t, err)

	// now setting the state to 'ClusterStateConnected' (to mock a successful external cluster connection)
	foundCeph.Status.State = cephv1.ClusterStateConnected
	err = reconciler.Client.Update(context.TODO(), foundCeph)
	assert.NoError(t, err)
	// call 'ensureNoobaaSystem()' to make sure it takes appropriate action
	// when ceph cluster is connected to an external cluster
	err = obj.ensureCreated(&reconciler, cr)
	assert.NoError(t, err)
	fNoobaa = &v1alpha1.NooBaa{}
	request.Name = "noobaa"
	// expectation is to get an appropriate Noobaa object
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, fNoobaa)
	assert.NoError(t, err)
}

func getReconciler(t *testing.T, objs ...runtime.Object) StorageClusterReconciler {
	registerObjs := []runtime.Object{&v1.StorageCluster{}}
	registerObjs = append(registerObjs, objs...)
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(registerObjs...).Build()

	return StorageClusterReconciler{
		Scheme:   scheme,
		Client:   client,
		platform: &Platform{},
	}
}

func TestNoobaaKMSConfiguration(t *testing.T) {
	allKMSArgs := []struct {
		testLabel             string
		kmsProvider           string
		kmsAddress            string
		clusterWideEncryption bool
		failureExpected       bool
	}{
		{testLabel: "case 1", kmsProvider: "vault",
			clusterWideEncryption: true, kmsAddress: "http://localhost:3053"},
		{testLabel: "case 2", kmsProvider: "vault",
			clusterWideEncryption: false, kmsAddress: "http://localhost:32123", failureExpected: true},
		// ocs-operator is agnostic to KMS Provider, here rook should be throwing error
		{testLabel: "case 3", kmsProvider: "newKMSProvider",
			clusterWideEncryption: true, kmsAddress: "http://127.0.0.1:15851"},
		// invalid test case, with an unreachable KMS address
		{testLabel: "case 4", kmsProvider: "vault", kmsAddress: "http://unearchable.url.location:3366", failureExpected: true},
	}
	for _, kmsArgs := range allKMSArgs {
		assertNoobaaKMSConfiguration(t, kmsArgs)
	}
}

func assertNoobaaKMSConfiguration(t *testing.T, kmsArgs struct {
	testLabel             string
	kmsProvider           string
	kmsAddress            string
	clusterWideEncryption bool
	failureExpected       bool
}) {
	ctxTodo := context.TODO()
	cr := createDefaultStorageCluster()
	// enable KMS to true
	cr.Spec.Encryption.KeyManagementService.Enable = true
	cr.Spec.Encryption.Enable = kmsArgs.clusterWideEncryption
	kmsCM := createDummyKMSConfigMap(kmsArgs.kmsProvider, kmsArgs.kmsAddress)
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	if err := reconciler.Client.Create(ctxTodo, kmsCM); err != nil {
		t.Errorf("Unable to create KMS configmap: %v, %v", err, kmsArgs.testLabel)
		t.FailNow()
	}
	reconciler.initializeImagesStatus(cr)
	// start a dummy server, if we are not expecting any errors
	if !kmsArgs.failureExpected {
		startServerAt(t, kmsArgs.kmsAddress)
	}

	var obj ocsCephCluster

	err := obj.ensureCreated(&reconciler, cr)
	if kmsArgs.failureExpected && err == nil {
		// case 1: if we are expecting a failure and returned error is 'nil'
		t.Errorf("Expecting the cephcluster creation to fail")
		t.FailNow()
	} else if !kmsArgs.failureExpected && err != nil {
		// case 2: if we are not expecting any failure, but received an error
		t.Errorf("CephCluster creation is not expected to fail: %v, %v", err, kmsArgs.testLabel)
		t.FailNow()
	} else if kmsArgs.failureExpected && err != nil {
		// case 3: if we are expecting a failure and an error is properly returned
		return
	}
	cephCluster := &cephv1.CephCluster{}
	err = reconciler.Client.Get(ctxTodo,
		types.NamespacedName{Name: generateNameForCephCluster(cr)},
		cephCluster)
	if err == nil {
		cephCluster.Status.State = cephv1.ClusterStateCreated
		err = reconciler.Client.Update(context.TODO(), cephCluster)
	}
	if err != nil {
		t.Errorf("CephCluster error: %v, %v", err, kmsArgs.testLabel)
		t.FailNow()
	}

	var objNoobaa ocsNoobaaSystem

	err = objNoobaa.ensureCreated(&reconciler, cr)
	assert.NoError(t, err, fmt.Sprintf("Failed to ensure Noobaa system: %v, %v", err, kmsArgs.testLabel))
	nb := &v1alpha1.NooBaa{}
	err = reconciler.Client.Get(ctxTodo, types.NamespacedName{Name: "noobaa"}, nb)
	assert.NoErrorf(t, err, "Failed to get Noobaa: %v, %v", err, kmsArgs.testLabel)
	// check the provided KMS ConfigMap data is passed on to CephCluster
	for k, v := range kmsCM.Data {
		assert.Equal(t, v, nb.Spec.Security.KeyManagementService.ConnectionDetails[k], fmt.Sprintf("Failed: %q. Expected values for key: %q, to be same", kmsArgs.testLabel, k))
	}
	assert.Equal(t, KMSTokenSecretName, nb.Spec.Security.KeyManagementService.TokenSecretName, fmt.Sprintf("Failed: %q. Expected the token-names tobe same", kmsArgs.testLabel))
}
