package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strconv"
	"testing"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var globalTestExternalResources = []ExternalResource{
	{
		Kind: "ConfigMap",
		Data: map[string]string{
			"maxMonId": "0",
			"data":     "a=10.20.30.40:1234",
			"mapping":  "{}",
		},
		Name: "rook-ceph-mon-endpoints",
	},
	{
		Kind: "Secret",
		Data: map[string]string{
			"userKey": "someUserKeyRBD==",
			"userID":  "csi-rbd-node",
		},
		Name: "rook-csi-rbd-node",
	},
	{
		Kind: "StorageClass",
		Data: map[string]string{
			"pool": "device_health_metrics",
		},
		Name: "ceph-rbd",
	},
	{
		Kind: "StorageClass",
		Data: map[string]string{
			"fsName": "myfs",
			"pool":   "myfs-data0",
		},
		Name: "cephfs",
	},
	{
		Kind: "StorageClass",
		Data: map[string]string{
			"endpoint": fmt.Sprintf("localhost:%d", generateRandomPort(10000, 30000)),
		},
		Name: "ceph-rgw",
	},
	{
		Kind: "CephCluster",
		Data: map[string]string{
			"MonitoringEndpoint": "127.0.0.1, localhost",
			"MonitoringPort":     fmt.Sprintf("%d", generateRandomPort(19000, 29000)),
		},
		Name: "monitoring-endpoint",
	},
}

func TestEnsureExternalStorageClusterResources(t *testing.T) {
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
	assertExpectedExternalResources(t, reconciler)
}

func createExternalCephClusterSecret(extResources []ExternalResource) (*corev1.Secret, error) {
	jsonBlob, err := json.Marshal(extResources)
	if err != nil {
		return nil, err
	}
	externalSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name: externalClusterDetailsSecret,
		},
		Data: map[string][]byte{
			externalClusterDetailsKey: jsonBlob,
		},
	}
	return externalSecret, err
}

func createExternalClusterReconciler(t *testing.T) StorageClusterReconciler {
	return createExternalClusterReconcilerFromCustomResources(t, globalTestExternalResources)
}

func createExternalClusterReconcilerFromCustomResources(
	t *testing.T, extResources []ExternalResource) StorageClusterReconciler {
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocsinit",
			Annotations: map[string]string{
				UninstallModeAnnotation: string(UninstallModeGraceful),
				CleanupPolicyAnnotation: string(CleanupPolicyDelete),
			},
			Finalizers:      []string{storageClusterFinalizer},
			OwnerReferences: []metav1.OwnerReference{{Name: "storage-test", Kind: "StorageSystem", APIVersion: "v1"}},
		},
		Spec: api.StorageClusterSpec{
			ExternalStorage: api.ExternalStorageClusterSpec{
				Enable: true,
			},
			Monitoring: &api.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyIgnore),
			},
		},
	}
	if extResource, err := findNamedResourceFromArray(extResources, "ceph-rgw"); err == nil {
		servEndpoint := extResource.Data["endpoint"]
		startServerAt(t, servEndpoint)
	}
	if extResource, err := findNamedResourceFromArray(extResources, "monitoring-endpoint"); err == nil {
		monEndpointIP := extResource.Data["MonitoringEndpoint"]
		monEndpointPort := extResource.Data["MonitoringPort"]
		if monEndpointIP != "" && monEndpointPort != "" {
			monEndpointIP = parseMonitoringIPs(monEndpointIP)[0]
			servEndpoint := net.JoinHostPort(monEndpointIP, monEndpointPort)
			startServerAt(t, servEndpoint)
		}
	}
	externalSecret, err := createExternalCephClusterSecret(extResources)
	if err != nil {
		t.Fatalf("failed to create external secret: %v", err)
	}
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	clientObjs := []client.Object{cr, externalSecret}
	for _, obj := range clientObjs {
		if err = reconciler.Client.Create(context.TODO(), obj); err != nil {
			t.Fatalf("failed to create a needed runtime object: %v", err)
		}
	}
	return reconciler
}

func assertExpectedExternalResources(t *testing.T, reconciler StorageClusterReconciler) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, sc)
	assert.NoError(t, err)

	externalSecret := &corev1.Secret{}
	request.Name = externalClusterDetailsSecret
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, externalSecret)
	assert.NoError(t, err)

	var data []ExternalResource
	err = json.Unmarshal(externalSecret.Data[externalClusterDetailsKey], &data)
	if err != nil {
		t.Errorf("fatal err %+v", err)
	}

	for _, expected := range data {
		request.Name = expected.Name
		switch expected.Kind {
		case "CephCluster":
			actual := &cephv1.CephCluster{}
			err := reconciler.Client.Get(context.TODO(),
				types.NamespacedName{Name: generateNameForCephCluster(sc)}, actual)
			assert.NoError(t, err)
			assert.True(t, actual.Spec.Monitoring.Enabled, "Expecting 'Monitoring' to be enabled")
			if uint16Port, err := strconv.ParseUint(expected.Data["MonitoringPort"], 10, 16); err == nil {
				assert.Equal(t, actual.Spec.Monitoring.ExternalMgrPrometheusPort, uint16(uint16Port))
			} else {
				assert.Zero(t, actual.Spec.Monitoring.ExternalMgrPrometheusPort, "Expected the port to be ZERO")
			}
		case "ConfigMap":
			actual := &corev1.ConfigMap{}
			err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actual)
			assert.NoError(t, err)
			for er := range expected.Data {
				assert.Equal(t, expected.Data[er], actual.Data[er])
			}
		case "Secret":
			actual := &corev1.Secret{}
			err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actual)
			assert.NoError(t, err)
			for er := range expected.Data {
				assert.Equal(t, []byte(expected.Data[er]), actual.Data[er])
			}
		case "StorageClass":
			actual := &storagev1.StorageClass{}
			request.Name = fmt.Sprintf("%s-%s", sc.Name, expected.Name)
			err := reconciler.Client.Get(context.TODO(), request.NamespacedName, actual)
			assert.NoError(t, err)
			// 'endpoint's are not required, as they are moved out to CephObjectStore
			delete(expected.Data, "endpoint")
			for param, value := range expected.Data {
				assert.Equal(t, value, actual.Parameters[param])
			}
			// Verify the RGW SC parameters in external mode are correct
			// The main difference between external and converged is the presence of an endpoint
			// and the absence of the "objectStoreName" parameter
			if actual.Name == "ocsinit-ceph-rgw" {
				assert.NotEmpty(t, actual.Parameters["region"], actual.Parameters)
				assert.NotContains(t, actual.Parameters["objectStoreName"], actual.Parameters)
				assert.Equal(t, actual.Parameters["region"], "us-east-1")
				assert.Equal(t, 3, len(actual.Parameters), actual.Parameters)
			}
		}
	}
}

// removeNamedResourceFromArray removes the first resource with 'Name' == 'name'
func removeNamedResourceFromArray(extArr []ExternalResource, name string) []ExternalResource {
	extArrLen := len(extArr)
	var i int
	for i = 0; i < extArrLen; i++ {
		extRsrc := extArr[i]
		if extRsrc.Name == name {
			break
		}
	}
	var newExtArr []ExternalResource
	newExtArr = append(newExtArr, extArr[:i]...)
	if i < extArrLen {
		newExtArr = append(newExtArr, extArr[i+1:]...)
	}
	return newExtArr
}

// updateNamedResourceInArray updates the provided 'extArr' with the given 'extRsrc' external resource
func updateNamedResourceInArray(extArr []ExternalResource, extRsrc ExternalResource) []ExternalResource {
	_, err := findNamedResourceFromArray(extArr, extRsrc.Name)
	if err == nil {
		extArr = removeNamedResourceFromArray(extArr, extRsrc.Name)
	}
	extArr = append(extArr, extRsrc)
	return extArr
}

func generateRandomPort(minPort, maxPort int) int {
	rand.New(rand.NewSource(time.Now().UnixNano()))
	portRange := minPort - maxPort
	if portRange < 0 {
		portRange *= -1
	}
	retPort := rand.Intn(portRange) + minPort
	return retPort
}

func TestOptionalExternalStorageClusterResources(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	optionalTestParams := []struct {
		label                     string
		resourceToBeRemoved       string
		expectedRookCephConfigVal string
	}{
		{
			label:               "RemoveRGW",
			resourceToBeRemoved: "ceph-rgw",
		},
		{
			label:               "RemoveCephFS",
			resourceToBeRemoved: "cephfs",
		},
	}

	for _, testParam := range optionalTestParams {
		t.Run(testParam.label, func(t *testing.T) {
			extResources := removeNamedResourceFromArray(globalTestExternalResources, testParam.resourceToBeRemoved)
			reconciler := createExternalClusterReconcilerFromCustomResources(t, extResources)
			result, err := reconciler.Reconcile(context.TODO(), request)
			assert.NoError(t, err)
			assert.Equal(t, reconcile.Result{}, result)
			// rest of the resources should be available
			assertExpectedExternalResources(t, reconciler)
			// make sure we are missing the provided resource
			assertMissingExternalResource(t, reconciler, testParam.resourceToBeRemoved)
			// make sure about the availability of 'CephObjectStore' according to the resource removed
			assertCephObjectStore(t, reconciler, testParam.resourceToBeRemoved)
		})
	}
}

func assertMissingExternalResource(t *testing.T, reconciler StorageClusterReconciler, resourceName string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, sc)
	assert.NoError(t, err)

	externalSecret := &corev1.Secret{}
	request.Name = externalClusterDetailsSecret
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, externalSecret)
	assert.NoError(t, err)

	var data []ExternalResource
	err = json.Unmarshal(externalSecret.Data[externalClusterDetailsKey], &data)
	if err != nil {
		t.Errorf("fatal err %+v", err)
	}
	actual := &storagev1.StorageClass{}
	request.Name = fmt.Sprintf("%s-%s", sc.Name, resourceName)
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, actual)
	// as the resource is missing, we are expecting an 'error'
	assert.Error(t, err)
}

func assertCephObjectStore(t *testing.T, reconciler StorageClusterReconciler, removedResource string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, sc)
	assert.NoError(t, err)
	expectedName := fmt.Sprintf("%s-cephobjectstore", sc.Name)
	request.Name = expectedName
	cObjS := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: expectedName,
		},
	}
	err = reconciler.Client.Get(context.TODO(), request.NamespacedName, cObjS)
	// if removed resource is 'ceph-rgw', we should not get CephObjectStore object
	if removedResource == "ceph-rgw" {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		extRs, err := reconciler.retrieveExternalSecretData(sc)
		assert.NoError(t, err)
		extR, err := findNamedResourceFromArray(extRs, "ceph-rgw")
		assert.NoError(t, err)
		hostFound, portFound, err := net.SplitHostPort(extR.Data["endpoint"])
		assert.NoError(t, err)
		if cObjS.Spec.Gateway.Port == 0 {
			assert.Equal(t, portFound, fmt.Sprintf("%d", cObjS.Spec.Gateway.SecurePort))
			assert.Equal(t, cephRgwTLSSecretKey, cObjS.Spec.Gateway.SSLCertificateRef)
		} else {
			assert.Equal(t, portFound, fmt.Sprintf("%d", cObjS.Spec.Gateway.Port))
		}
		// length of 'ExternalRgwEndpoints' should be at least 1
		assert.True(t, len(cObjS.Spec.Gateway.ExternalRgwEndpoints) > 0, true)
		// and the first IP/Hostname should be that of the host we passed from 'ceph-rgw' resource
		assert.Equal(t, hostFound, cObjS.Spec.Gateway.ExternalRgwEndpoints[0].Hostname)
	}
}

func TestExternalResourceReconcile(t *testing.T) {
	reconciler := createExternalClusterReconciler(t)
	assertReconciliationOfExternalResource(t, reconciler)
}

func assertReconciliationOfExternalResource(t *testing.T, reconciler StorageClusterReconciler) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	ctx := context.TODO()

	// first reconcile, which sets everything in place
	result, err := reconciler.Reconcile(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	sc := &api.StorageCluster{}
	err = reconciler.Client.Get(ctx, request.NamespacedName, sc)
	assert.NoError(t, err)
	firstExtSecretChecksum := sc.Status.ExternalSecretHash

	extRsrcs, err := reconciler.retrieveExternalSecretData(sc)
	assert.NoError(t, err)
	rgwRsrc, err := findNamedResourceFromArray(extRsrcs, cephRgwStorageClassName)
	assert.NoError(t, err)
	// change 'rgw-endpoint'
	rgwRsrc.Data[externalCephRgwEndpointKey] = fmt.Sprintf("localhost:%d", generateRandomPort(20000, 30000))
	// start a dummy / local server at the endpoint
	startServerAt(t, rgwRsrc.Data[externalCephRgwEndpointKey])
	extRsrcs = updateNamedResourceInArray(extRsrcs, rgwRsrc)
	// create and update external secret with new changes
	extSecret, err := createExternalCephClusterSecret(extRsrcs)
	assert.NoError(t, err)
	secret := corev1.Secret{}
	err = reconciler.Client.Get(ctx, types.NamespacedName{Name: externalClusterDetailsSecret, Namespace: ""}, &secret)
	assert.NoError(t, err)
	extSecret.ObjectMeta = secret.ObjectMeta
	err = reconciler.Client.Update(ctx, extSecret)
	assert.NoError(t, err)

	// second reconcile on same 'reconciler', we should have expected/changed resources
	result, err = reconciler.Reconcile(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	// get the updated storagecluster object after second reconciliation
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(ctx, request.NamespacedName, sc)
	assert.NoError(t, err)
	secondExtSecretChecksum := sc.Status.ExternalSecretHash
	// as there are changes, first and second checksums should not match
	assert.NotEqual(t, firstExtSecretChecksum, secondExtSecretChecksum)

	// third reconcile on same 'reconciler', without any change in the resources
	result, err = reconciler.Reconcile(ctx, request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	// get the updated storagecluster object after third reconciliation
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(ctx, request.NamespacedName, sc)
	assert.NoError(t, err)
	thirdExtSecretChecksum := sc.Status.ExternalSecretHash
	// as there are no changes, second and third checksums should match
	assert.Equal(t, secondExtSecretChecksum, thirdExtSecretChecksum)

}

func TestExternalMonitoringResources(t *testing.T) {
	type testResources struct {
		ExternalResource
		Label                   string
		ReconcileExpectedToFail bool
	}
	monAddedExternalResources := []testResources{
		{
			ExternalResource: ExternalResource{
				Kind: "CephCluster",
				Data: map[string]string{
					"MonitoringEndpoint": "127.0.0.1",
					"MonitoringPort":     fmt.Sprint(generateRandomPort(30000, 40000)),
				},
				Name: "monitoring-endpoint",
			},
			Label:                   "ValidEndpointAndPort",
			ReconcileExpectedToFail: false,
		},
		{
			ExternalResource: ExternalResource{
				Kind: "CephCluster",
				Data: map[string]string{
					"MonitoringEndpoint": "127.0.0.1",
				},
				Name: "monitoring-endpoint",
			},
			Label:                   "ValidEndpointWithoutPort",
			ReconcileExpectedToFail: false,
		},
		{
			ExternalResource: ExternalResource{
				Kind: "CephCluster",
				Data: map[string]string{
					"MonitoringEndpoint": "127.0.0.1",
					"MonitoringPort":     "abcde",
				},
				Name: "monitoring-endpoint",
			},
			Label:                   "InvalidPort",
			ReconcileExpectedToFail: true,
		},
	}

	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	for _, extR := range monAddedExternalResources {
		t.Run(extR.Label, func(t *testing.T) {
			extRArr := updateNamedResourceInArray(globalTestExternalResources, extR.ExternalResource)

			reconciler := createExternalClusterReconcilerFromCustomResources(t, extRArr)
			result, err := reconciler.Reconcile(context.TODO(), request)
			if extR.ReconcileExpectedToFail && err != nil {
				return
			}
			if ok := assert.NoError(t, err); !ok {
				t.Fatalf("Reconcile Error: %v", err)
			}
			assert.Equal(t, reconcile.Result{}, result)
			assertExpectedExternalResources(t, reconciler)
		})
	}
}

func TestErasureCodedExternalResources(t *testing.T) {
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocsinit",
			Namespace: "openshift-storage",
		},
		Spec: api.StorageClusterSpec{
			ExternalStorage: api.ExternalStorageClusterSpec{
				Enable: true,
			},
			Monitoring: &api.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyIgnore),
			},
		},
	}
	externalResource := []ExternalResource{
		{
			Name: "ceph-rbd",
			Kind: "StorageClass",
			Data: map[string]string{
				"pool": GenerateNameForCephBlockPool(cr),
			},
		},
		{
			Name: "ceph-rbd-ec",
			Kind: "StorageClass",
			Data: map[string]string{
				"dataPool": "ec-data-pool",
				"pool":     "replicated-metadata-pool",
			},
		},
	}

	for _, extR := range externalResource {
		t.Run(extR.Name, func(t *testing.T) {
			actualSC := newCephBlockPoolStorageClassConfiguration(cr)
			// To override the values for external cluster
			for k, v := range extR.Data {
				actualSC.storageClass.Parameters[k] = v
			}
			assert.NotEmpty(t, actualSC.storageClass.Parameters["clusterID"])
			assert.Equal(t, extR.Data["dataPool"], actualSC.storageClass.Parameters["dataPool"])
			assert.Equal(t, extR.Data["pool"], actualSC.storageClass.Parameters["pool"])
			assert.NotEmpty(t, actualSC.storageClass.Parameters["csi.storage.k8s.io/provisioner-secret-name"])
			assert.NotEmpty(t, actualSC.storageClass.Parameters["csi.storage.k8s.io/provisioner-secret-namespace"])
		})
	}
}
