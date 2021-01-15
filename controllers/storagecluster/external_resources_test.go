package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"regexp"
	"strconv"
	"testing"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	api "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
}

func TestEnsureExternalStorageClusterResources(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	reconciler := createExternalClusterReconciler(t)
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)
}

func newRookCephOperatorConfig(namespace string) *corev1.ConfigMap {
	var defaultCSIToleration = `
- key: ` + defaults.NodeTolerationKey + `
  operator: Equal
  value: "true"
  effect: NoSchedule`
	config := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookCephOperatorConfigName,
			Namespace: namespace,
		},
	}
	data := make(map[string]string)
	data["CSI_PROVISIONER_TOLERATIONS"] = defaultCSIToleration
	data["CSI_PLUGIN_TOLERATIONS"] = defaultCSIToleration
	data["CSI_LOG_LEVEL"] = "5"
	config.Data = data
	return config
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
		},
		Spec: api.StorageClusterSpec{
			ExternalStorage: api.ExternalStorageClusterSpec{
				Enable: true,
			},
		},
	}
	if extResource, err := findNamedResourceFromArray(extResources, "ceph-rgw"); err == nil {
		startServerAt(extResource.Data["endpoint"])
	}
	if extResource, err := findNamedResourceFromArray(extResources, "monitoring-endpoint"); err == nil {
		monEndpointIP := extResource.Data["MonitoringEndpoint"]
		monEndpointPort := extResource.Data["MonitoringPort"]
		if monEndpointIP != "" && monEndpointPort != "" {
			startServerAt(net.JoinHostPort(monEndpointIP, monEndpointPort))
		}
	}
	externalSecret, err := createExternalCephClusterSecret(extResources)
	if err != nil {
		t.Fatalf("failed to create external secret: %v", err)
	}
	rookCephConfig := newRookCephOperatorConfig("")
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	runtimeObjs := []runtime.Object{cr, externalSecret, rookCephConfig}
	for _, obj := range runtimeObjs {
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

// findNamedResourceFromArray retrieves the 'ExternalResource' with provided 'name'
func findNamedResourceFromArray(extArr []ExternalResource, name string) (ExternalResource, error) {
	for _, extR := range extArr {
		if extR.Name == name {
			return extR, nil
		}
	}
	return ExternalResource{}, fmt.Errorf("Unable to retrieve %q external resource", name)
}

// removeNamedResourceFromArray removes the first resource with 'Name' == 'name'
func removeNamedResourceFromArray(extArr []ExternalResource, name string) []ExternalResource {
	extArrLen := len(extArr)
	var indx int
	for indx = 0; indx < extArrLen; indx++ {
		extRsrc := extArr[indx]
		if extRsrc.Name == name {
			break
		}
	}
	var newExtArr []ExternalResource
	newExtArr = append(newExtArr, extArr[:indx]...)
	if indx < extArrLen {
		newExtArr = append(newExtArr, extArr[indx+1:]...)
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

func startServerAt(endpoint string) <-chan error {
	var doneChan = make(chan error)
	go func(doneChan chan<- error, endpoint string) {
		defer close(doneChan)
		rxp := regexp.MustCompile(`^http[s]?://`)
		// remove any http or https protocols from the endpoint string
		endpoint = rxp.ReplaceAllString(endpoint, "")
		ln, err := net.Listen("tcp4", endpoint)
		if err != nil {
			doneChan <- err
			return
		}
		defer ln.Close()
		conn, err := ln.Accept()
		if err != nil {
			doneChan <- err
			return
		}
		defer conn.Close()
		doneChan <- nil
	}(doneChan, endpoint)
	return doneChan
}

func generateRandomPort(minPort, maxPort int) int {
	rand.Seed(time.Now().UnixNano())
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
		resourceToBeRemoved       string
		expectedRookCephConfigVal string
	}{
		{resourceToBeRemoved: "ceph-rgw", expectedRookCephConfigVal: "true"},
		{resourceToBeRemoved: "cephfs", expectedRookCephConfigVal: "false"},
	}

	for _, testParam := range optionalTestParams {
		extResources := removeNamedResourceFromArray(globalTestExternalResources, testParam.resourceToBeRemoved)
		reconciler := createExternalClusterReconcilerFromCustomResources(t, extResources)
		result, err := reconciler.Reconcile(request)
		assert.NoError(t, err)
		assert.Equal(t, reconcile.Result{}, result)
		// rest of the resources should be available
		assertExpectedExternalResources(t, reconciler)
		// make sure we are missing the provided resource
		assertMissingExternalResource(t, reconciler, testParam.resourceToBeRemoved)
		// make sure that we have expected rook ceph config value
		assertRookCephOperatorConfigValue(t, reconciler, testParam.expectedRookCephConfigVal)
		// make sure about the availability of 'CephObjectStore' according to the resource removed
		assertCephObjectStore(t, reconciler, testParam.resourceToBeRemoved)
	}
}

func assertRookCephOperatorConfigValue(t *testing.T, reconciler StorageClusterReconciler, checkValue string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, sc)
	assert.NoError(t, err)
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err = reconciler.Client.Get(context.TODO(),
		types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: sc.ObjectMeta.Namespace},
		rookCephOperatorConfig)
	assert.NoErrorf(t, err, "Unable to get '%s' config", rookCephOperatorConfigName)
	assert.Truef(t,
		rookCephOperatorConfig.Data[rookEnableCephFSCSIKey] == checkValue,
		"'%s' key is supposed to be '%s'", rookEnableCephFSCSIKey, checkValue)
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
		assert.Equal(t, portFound, fmt.Sprintf("%d", cObjS.Spec.Gateway.Port))
		// length of 'ExternalRgwEndpoints' should be atleast 1
		assert.True(t, len(cObjS.Spec.Gateway.ExternalRgwEndpoints) > 0, true)
		// and the first IP should be that of the host we passed from 'ceph-rgw' resource
		assert.Equal(t, hostFound, cObjS.Spec.Gateway.ExternalRgwEndpoints[0].IP)
	}
}

func TestExternalResourceReconcile(t *testing.T) {
	reconciler := createExternalClusterReconciler(t)
	assertReconciliationOfExternalResource(t, reconciler)
}

// nolint
func assertReconciliationOfExternalResource(t *testing.T, reconciler StorageClusterReconciler) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}

	// first reconcile, which sets everything in place
	result, err := reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	sc := &api.StorageCluster{}
	err = reconciler.Client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)
	firstExtSecretChecksum := sc.Status.ExternalSecretHash

	extRsrcs, err := reconciler.retrieveExternalSecretData(sc)
	assert.NoError(t, err)
	rgwRsrc, err := findNamedResourceFromArray(extRsrcs, cephRgwStorageClassName)
	assert.NoError(t, err)
	// change 'rgw-endpoint'
	rgwRsrc.Data[externalCephRgwEndpointKey] = fmt.Sprintf("localhost:%d", generateRandomPort(20000, 30000))
	// start a dummy / local server at the endpoint
	startServerAt(rgwRsrc.Data[externalCephRgwEndpointKey])
	extRsrcs = updateNamedResourceInArray(extRsrcs, rgwRsrc)
	// create and update external secret with new changes
	extSecret, err := createExternalCephClusterSecret(extRsrcs)
	assert.NoError(t, err)
	secret := corev1.Secret{}
	reconciler.Client.Get(nil, types.NamespacedName{Name: externalClusterDetailsSecret, Namespace: ""}, &secret)
	assert.NoError(t, err)
	extSecret.ObjectMeta = secret.ObjectMeta
	err = reconciler.Client.Update(nil, extSecret)
	assert.NoError(t, err)

	// second reconcile on same 'reconciler', we should have expected/changed resources
	result, err = reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	// get the updated storagecluster object after second reconciliation
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)
	secondExtSecretChecksum := sc.Status.ExternalSecretHash
	// as there are changes, first and second checksums should not match
	assert.NotEqual(t, firstExtSecretChecksum, secondExtSecretChecksum)

	// third reconcile on same 'reconciler', without any change in the resources
	result, err = reconciler.Reconcile(request)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
	assertExpectedExternalResources(t, reconciler)

	// get the updated storagecluster object after third reconciliation
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(nil, request.NamespacedName, sc)
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
			Label:                   "A passing case, with valid args",
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
			Label:                   "Another passing case, without an explicit port, in which rook will provide a default port number",
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
			Label:                   "A failing case, which has an invalid port",
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
		extRArr := updateNamedResourceInArray(globalTestExternalResources, extR.ExternalResource)

		reconciler := createExternalClusterReconcilerFromCustomResources(t, extRArr)
		result, err := reconciler.Reconcile(request)
		if extR.ReconcileExpectedToFail && err != nil {
			continue
		}
		if ok := assert.NoError(t, err); !ok {
			t.Fatalf("Reconcile Error: %v", err)
		}
		assert.Equal(t, reconcile.Result{}, result)
		assertExpectedExternalResources(t, reconciler)
	}
}
