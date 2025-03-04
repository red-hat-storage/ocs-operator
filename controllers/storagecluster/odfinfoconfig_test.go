package storagecluster

import (
	"os"
	"testing"

	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	ocsversion "github.com/red-hat-storage/ocs-operator/v4/version"
	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"gotest.tools/v3/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestOdfInfoConfig(t *testing.T) {

	const namespace = "storage-test-ns"
	os.Setenv(util.OperatorNamespaceEnvVar, namespace)

	testTable := []struct {
		label                        string
		expectedPublicNetworkValue   string
		storageSystemOwnerRef        metav1.OwnerReference
		ocsVersion                   string
		deploymentType               string
		storageClusterNamespacedName types.NamespacedName
		cephClusterFSID              string
		numConnectedClients          int
		shouldFail                   bool
	}{
		{
			label: "Case #1: Green path no StorageConsumers",
			storageSystemOwnerRef: metav1.OwnerReference{
				Name:       "storage-test",
				Kind:       "StorageSystem",
				APIVersion: "v1",
			},
			ocsVersion:                   ocsversion.Version,
			deploymentType:               odfDeploymentTypeInternal,
			storageClusterNamespacedName: types.NamespacedName{Name: "storage-test", Namespace: namespace},
			cephClusterFSID:              cephFSID,
			numConnectedClients:          0,
			shouldFail:                   false,
		},
		{
			label: "Case #2: Green path with StorageConsumer",
			storageSystemOwnerRef: metav1.OwnerReference{
				Name:       "storage-test",
				Kind:       "StorageSystem",
				APIVersion: "v1",
			},
			ocsVersion:                   ocsversion.Version,
			deploymentType:               odfDeploymentTypeInternal,
			storageClusterNamespacedName: types.NamespacedName{Name: "storage-test", Namespace: namespace},
			cephClusterFSID:              cephFSID,
			numConnectedClients:          1,
			shouldFail:                   false,
		},
		{
			label:                 "Case #3: StorageCluster has blank StorageSystem owner ref",
			storageSystemOwnerRef: metav1.OwnerReference{},
			shouldFail:            true,
		},
		{
			label: "Case #4: StorageCluster has no owner ref of kind StorageSystem",
			storageSystemOwnerRef: metav1.OwnerReference{
				Name:       "storage-test",
				APIVersion: "v1",
			},
			storageClusterNamespacedName: types.NamespacedName{Name: "storage-test", Namespace: namespace},
			shouldFail:                   true,
		},
	}

	for i, testCase := range testTable {
		t.Logf("Case #%+v", i+1)
		// setup the mocks

		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      OdfInfoConfigMapName,
				Namespace: namespace,
			},
		}

		sc := &api.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testCase.storageClusterNamespacedName.Name,
				Namespace: testCase.storageClusterNamespacedName.Namespace,
				OwnerReferences: []metav1.OwnerReference{
					testCase.storageSystemOwnerRef}},
		}

		storageConsumer := &v1alpha1.StorageConsumer{}
		if testCase.numConnectedClients > 0 {
			storageConsumer = &v1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{Name: "storage-consumer-test", Namespace: namespace},
				Spec: v1alpha1.StorageConsumerSpec{
					Enable: true,
				},
				Status: v1alpha1.StorageConsumerStatus{
					Client: v1alpha1.ClientStatus{
						OperatorVersion: testCase.ocsVersion,
						ClusterID:       "",
						ClusterName:     testCase.storageClusterNamespacedName.Name,
						Name:            "storage-consumer-test",
					},
				},
			}
		}
		r := createFakeStorageClusterReconciler(t, storageConsumer)

		assert.Equal(t, r.OperatorNamespace, namespace)
		odfInfoConfigReconciler := &odfInfoConfig{}
		_, err := odfInfoConfigReconciler.ensureCreated(&r, sc)
		if err != nil {
			if !testCase.shouldFail {
				assert.NilError(t, err, "ensure created failed")
			} else {
				continue
			}
		}
		// get the output
		err = r.Client.Get(r.ctx, client.ObjectKeyFromObject(configMap), configMap)
		assert.NilError(t, err, "expected to find configmap %q: %+v", OdfInfoConfigMapName, err)

		// compare with the expected results
		odfInfoDataOfStorageClusterKey := v1alpha1.OdfInfoData{}
		err = yaml.Unmarshal([]byte(configMap.Data[odfInfoConfigReconciler.getOdfInfoKeyName(sc)]),
			&odfInfoDataOfStorageClusterKey)
		assert.NilError(t, err, "Expected unmarshalling of OdfInfoConfig's data")
		assert.Equal(t, odfInfoDataOfStorageClusterKey.DeploymentType, testCase.deploymentType)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.Version, testCase.ocsVersion)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.StorageSystemName, testCase.storageSystemOwnerRef.Name)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.StorageCluster.NamespacedName,
			testCase.storageClusterNamespacedName)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.StorageCluster.CephClusterFSID, testCase.cephClusterFSID)
		assert.Equal(t, len(odfInfoDataOfStorageClusterKey.Clients), testCase.numConnectedClients)
	}

}
