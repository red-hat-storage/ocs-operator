package storagecluster

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
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
	// TODO (leelavg): recheck after enabling upgrades
	t.Skip("testcase is flawed, it seems both the actual & expected are test generated")

	const namespace = "storage-test-ns"
	_ = os.Setenv(util.OperatorNamespaceEnvVar, namespace)

	testTable := []struct {
		label                        string
		expectedPublicNetworkValue   string
		ocsVersion                   string
		deploymentType               string
		storageClusterNamespacedName types.NamespacedName
		cephClusterFSID              string
		numConnectedClients          int
	}{
		{
			label:                        "Case #1: Green path no StorageConsumers",
			ocsVersion:                   ocsversion.Version,
			deploymentType:               odfDeploymentTypeInternal,
			storageClusterNamespacedName: types.NamespacedName{Name: "storage-test", Namespace: namespace},
			cephClusterFSID:              cephFSID,
			numConnectedClients:          0,
		},
		{
			label:                        "Case #2: Green path with StorageConsumer",
			ocsVersion:                   ocsversion.Version,
			deploymentType:               odfDeploymentTypeInternal,
			storageClusterNamespacedName: types.NamespacedName{Name: "storage-test", Namespace: namespace},
			cephClusterFSID:              cephFSID,
			numConnectedClients:          1,
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
			},
		}

		storageConsumer := &v1alpha1.StorageConsumer{}
		if testCase.numConnectedClients > 0 {
			storageConsumer = &v1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{Name: "storage-consumer-test", Namespace: namespace},
				Spec: v1alpha1.StorageConsumerSpec{
					Enable: true,
				},
				Status: v1alpha1.StorageConsumerStatus{
					Client: &v1alpha1.ClientStatus{
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
		_, err := odfInfoConfigReconciler.ensureCreated(r, sc)
		assert.NilError(t, err)
		// get the output
		err = r.Get(r.ctx, client.ObjectKeyFromObject(configMap), configMap)
		assert.NilError(t, err, "expected to find configmap %q: %+v", OdfInfoConfigMapName, err)

		// compare with the expected results
		odfInfoDataOfStorageClusterKey := v1alpha1.OdfInfoData{}
		err = yaml.Unmarshal([]byte(configMap.Data[odfInfoConfigReconciler.getOdfInfoKeyName(sc)]),
			&odfInfoDataOfStorageClusterKey)
		assert.NilError(t, err, "Expected unmarshalling of OdfInfoConfig's data")
		assert.Equal(t, odfInfoDataOfStorageClusterKey.DeploymentType, testCase.deploymentType)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.Version, testCase.ocsVersion)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.StorageCluster.NamespacedName,
			testCase.storageClusterNamespacedName)
		assert.Equal(t, odfInfoDataOfStorageClusterKey.StorageCluster.CephClusterFSID, testCase.cephClusterFSID)
		assert.Equal(t, len(odfInfoDataOfStorageClusterKey.Clients), testCase.numConnectedClients)
	}

}

func TestCreateClientCertAndCA(t *testing.T) {
	const namespace = "storage-test-ns"
	t.Setenv(util.OperatorNamespaceEnvVar, namespace)

	storageCluster := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "storage-test",
			Namespace: namespace,
			UID:       types.UID("storage-cluster-uid"),
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, storageCluster)
	reconciler.ctx = context.Background()

	odfInfo := &odfInfoConfig{}
	result, err := odfInfo.ensureCreated(reconciler, storageCluster)
	assert.NilError(t, err)
	assert.Assert(t, result.IsZero())

	secret := &corev1.Secret{}
	err = reconciler.Get(context.Background(), types.NamespacedName{Name: odfInfoClientCertName, Namespace: storageCluster.Namespace}, secret)
	assert.NilError(t, err)
	assert.Assert(t, strings.HasPrefix(secret.Annotations[defaults.CertIdentityAnnotation], defaults.LocalPeerCNPrefix+"-"))
	if len(secret.OwnerReferences) != 1 {
		t.Fatalf("expected one secret owner reference, got %d", len(secret.OwnerReferences))
	}
	assert.Equal(t, storageCluster.UID, secret.OwnerReferences[0].UID)

	caConfigMap := &corev1.ConfigMap{}
	err = reconciler.Get(context.Background(), types.NamespacedName{Name: odfInfoClientCAName, Namespace: storageCluster.Namespace}, caConfigMap)
	assert.NilError(t, err)
	assert.Equal(t, string(secret.Data["tls.crt"]), caConfigMap.Data["ca.crt"])
	if len(caConfigMap.OwnerReferences) != 1 {
		t.Fatalf("expected one CA ConfigMap owner reference, got %d", len(caConfigMap.OwnerReferences))
	}
	assert.Equal(t, storageCluster.UID, caConfigMap.OwnerReferences[0].UID)

	odfInfoConfigMap := &corev1.ConfigMap{}
	err = reconciler.Get(context.Background(), types.NamespacedName{Name: OdfInfoConfigMapName, Namespace: namespace}, odfInfoConfigMap)
	assert.NilError(t, err)
	odfInfoData := v1alpha1.OdfInfoData{}
	err = yaml.Unmarshal([]byte(odfInfoConfigMap.Data[odfInfo.getOdfInfoKeyName(storageCluster)]), &odfInfoData)
	assert.NilError(t, err)
	assert.Equal(t, odfInfoData.TLSInfo.ClientSAN, secret.Annotations[defaults.CertIdentityAnnotation])
	assert.Equal(t, odfInfoData.TLSInfo.ClientCMName, caConfigMap.Name)
	assert.Equal(t, odfInfoData.TLSInfo.ClientCertName, secret.Name)
}
