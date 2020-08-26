package storagecluster

import (
	"encoding/json"
	"fmt"
	"net"
	"testing"

	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var ExternalResources = []ExternalResource{
	ExternalResource{
		Kind: "ConfigMap",
		Data: map[string]string{
			"maxMonId": "0",
			"data":     "a=10.20.30.40:1234",
			"mapping":  "{}",
		},
		Name: "rook-ceph-mon-endpoints",
	},
	ExternalResource{
		Kind: "Secret",
		Data: map[string]string{
			"userKey": "someUserKeyRBD==",
			"userID":  "csi-rbd-node",
		},
		Name: "rook-csi-rbd-node",
	},
	ExternalResource{
		Kind: "StorageClass",
		Data: map[string]string{
			"pool": "device_health_metrics",
		},
		Name: "ceph-rbd",
	},
	ExternalResource{
		Kind: "StorageClass",
		Data: map[string]string{
			"fsName": "myfs",
			"pool":   "myfs-data0",
		},
		Name: "cephfs",
	},
	ExternalResource{
		Kind: "StorageClass",
		Data: map[string]string{
			"endpoint": "10.20.30.40:50",
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

func createExternalClusterReconciler(t *testing.T) ReconcileStorageCluster {
	return createExternalClusterReconcilerFromCustomResources(t, ExternalResources)
}

func createExternalClusterReconcilerFromCustomResources(
	t *testing.T, extResources []ExternalResource) ReconcileStorageCluster {
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
	externalSecret, err := createExternalCephClusterSecret(extResources)
	if err != nil {
		t.Fatalf("failed to create external secret: %v", err)
	}
	rookCephConfig := newRookCephOperatorConfig("")
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	runtimeObjs := []runtime.Object{cr, externalSecret, rookCephConfig}
	for _, obj := range runtimeObjs {
		if err = reconciler.client.Create(nil, obj); err != nil {
			t.Fatalf("failed to create a needed runtime object: %v", err)
		}
	}
	return reconciler
}

func assertExpectedExternalResources(t *testing.T, reconciler ReconcileStorageCluster) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)

	externalSecret := &corev1.Secret{}
	request.Name = externalClusterDetailsSecret
	err = reconciler.client.Get(nil, request.NamespacedName, externalSecret)
	assert.NoError(t, err)

	var data []ExternalResource
	err = json.Unmarshal(externalSecret.Data[externalClusterDetailsKey], &data)
	if err != nil {
		t.Errorf("fatal err %+v", err)
	}

	for _, expected := range data {
		request.Name = expected.Name
		switch expected.Kind {
		case "ConfigMap":
			actual := &corev1.ConfigMap{}
			err := reconciler.client.Get(nil, request.NamespacedName, actual)
			assert.NoError(t, err)
			for er := range expected.Data {
				assert.Equal(t, expected.Data[er], actual.Data[er])
			}
		case "Secret":
			actual := &corev1.Secret{}
			err := reconciler.client.Get(nil, request.NamespacedName, actual)
			assert.NoError(t, err)
			for er := range expected.Data {
				assert.Equal(t, []byte(expected.Data[er]), actual.Data[er])
			}
		case "StorageClass":
			actual := &storagev1.StorageClass{}
			request.Name = fmt.Sprintf("%s-%s", sc.Name, expected.Name)
			err := reconciler.client.Get(nil, request.NamespacedName, actual)
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
		extResources := removeNamedResourceFromArray(ExternalResources, testParam.resourceToBeRemoved)
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

func assertRookCephOperatorConfigValue(t *testing.T, reconciler ReconcileStorageCluster, checkValue string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err = reconciler.client.Get(nil,
		types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: sc.ObjectMeta.Namespace},
		rookCephOperatorConfig)
	assert.NoErrorf(t, err, "Unable to get '%s' config", rookCephOperatorConfigName)
	assert.Truef(t,
		rookCephOperatorConfig.Data[rookEnableCephFSCSIKey] == checkValue,
		"'%s' key is supposed to be '%s'", rookEnableCephFSCSIKey, checkValue)
}

func assertMissingExternalResource(t *testing.T, reconciler ReconcileStorageCluster, resourceName string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)

	externalSecret := &corev1.Secret{}
	request.Name = externalClusterDetailsSecret
	err = reconciler.client.Get(nil, request.NamespacedName, externalSecret)
	assert.NoError(t, err)

	var data []ExternalResource
	err = json.Unmarshal(externalSecret.Data[externalClusterDetailsKey], &data)
	if err != nil {
		t.Errorf("fatal err %+v", err)
	}
	actual := &storagev1.StorageClass{}
	request.Name = fmt.Sprintf("%s-%s", sc.Name, resourceName)
	err = reconciler.client.Get(nil, request.NamespacedName, actual)
	// as the resource is missing, we are expecting an 'error'
	assert.Error(t, err)
}

func assertCephObjectStore(t *testing.T, reconciler ReconcileStorageCluster, removedResource string) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	sc := &api.StorageCluster{}
	err := reconciler.client.Get(nil, request.NamespacedName, sc)
	assert.NoError(t, err)
	expectedName := fmt.Sprintf("%s-external-cephobjectstore", sc.Name)
	request.Name = expectedName
	cObjS := &cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name: expectedName,
		},
	}
	err = reconciler.client.Get(nil, request.NamespacedName, cObjS)
	// if removed resource is 'ceph-rgw', we should not get CephObjectStore object
	if removedResource == "ceph-rgw" {
		assert.Error(t, err)
	} else {
		assert.NoError(t, err)
		extRs, err := reconciler.retrieveExternalSecretData(sc, reconciler.reqLogger)
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
