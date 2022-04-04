package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"

	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	"k8s.io/apimachinery/pkg/runtime"
)

type externalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}

var mockExtR = map[string]*externalResource{
	"rook-ceph-mon-endpoints": {
		Name: "rook-ceph-mon-endpoints",
		Kind: "ConfigMap",
		Data: map[string]string{
			"data":     "a=10.99.45.27:6789",
			"maxMonId": "0",
			"mapping":  "{}",
		},
	},
	"rook-ceph-mon": {
		Name: "rook-ceph-mon",
		Kind: "Secret",
		Data: map[string]string{
			"ceph-username": "client.995e66248ad3e8642de868f461cdd827",
			"fsid":          "b88c2d78-9de9-4227-9313-a63f62f78743",
			"mon-secret":    "mon-secret",
			"ceph-secret":   "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
		},
	},
	"monitoring-endpoint": {
		Name: "monitoring-endpoint",
		Kind: "CephCluster",
		Data: map[string]string{
			"MonitoringEndpoint": "10.105.164.231",
			"MonitoringPort":     "9283",
		},
	},
	"rook-ceph-client-995e66248ad3e8642de868f461cdd827": {
		Name: "rook-ceph-client-995e66248ad3e8642de868f461cdd827",
		Kind: "Secret",
		Data: map[string]string{
			"userID":  "995e66248ad3e8642de868f461cdd827",
			"userKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
		},
	},
}

var (
	consumerResource = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer", UID: "uid"},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{
				{
					Name: "995e66248ad3e8642de868f461cdd827",
					Kind: "CephClient",
				},
			},
			State: ocsv1alpha1.StorageConsumerStateReady,
		},
	}

	consumerResource1 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer1", UID: "uid1"},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateFailed,
		},
	}

	consumerResource2 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer2", UID: "uid2"},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateConfiguring,
		},
	}
	consumerResource3 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer3", UID: "uid3"},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateDeleting,
		},
	}
	consumerResource4 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer4", UID: "uid4"},
		Status:     ocsv1alpha1.StorageConsumerStatus{},
	}

	consumerResource5 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{Name: "consumer5", UID: "uid5"},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{{}},
			State:         ocsv1alpha1.StorageConsumerStateReady,
		},
	}
)

func TestGetExternalResources(t *testing.T) {
	ctx := context.TODO()
	objects := []runtime.Object{
		&v1.ConfigMap{},
		&v1.Secret{},
		&rookCephv1.CephClient{},
		consumerResource,
		consumerResource1,
		consumerResource2,
		consumerResource3,
		consumerResource4,
		consumerResource5,
	}

	client := newFakeClient(t, objects...)
	consumerManager, err := newConsumerManager(ctx, client, "openshift-storage")
	assert.NoError(t, err)

	_, err = consumerManager.Create(ctx, "consumer", "ticket", resource.MustParse("1G"))
	assert.NoError(t, err)

	port, _ := strconv.Atoi("9283")
	mgrpod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-mgr-test",
			Namespace: "openshift-storage",
			Labels: map[string]string{
				"app": "rook-ceph-mgr",
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{{Name: "mgr", Ports: []v1.ContainerPort{{Name: "http-metrics", ContainerPort: int32(port)}}}},
		},
		Status: v1.PodStatus{
			HostIP: "10.105.164.231",
		},
	}

	err = client.Create(ctx, &mgrpod)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:    client,
		namespace: "openshift-storage",
		consumerManager: &ocsConsumerManager{
			nameByUID: map[types.UID]string{
				consumerResource.UID: consumerResource.Name,
			},
			client: client,
		},
	}

	cephClient := &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "995e66248ad3e8642de868f461cdd827",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageCephUserTypeAnnotation: "healthchecker",
				controllers.StorageClaimAnnotation:        "global",
				controllers.StorageConsumerAnnotation:     "consumer",
			},
		},
		Status: &rookCephv1.CephClientStatus{
			Info: map[string]string{
				"secretName": "rook-ceph-client-995e66248ad3e8642de868f461cdd827",
			},
		},
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rook-ceph-client-995e66248ad3e8642de868f461cdd827", Namespace: server.namespace},
		Data: map[string][]byte{
			"995e66248ad3e8642de868f461cdd827": []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	assert.NoError(t, client.Create(ctx, cephClient))
	assert.NoError(t, client.Create(ctx, secret))

	monCm, monSc := createMonConfigMapAndSecret(server)
	assert.NoError(t, client.Create(ctx, monCm))
	assert.NoError(t, client.Create(ctx, monSc))

	// When ocsv1alpha1.StorageConsumerStateReady
	req := pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource.UID),
	}
	storageConRes, err := server.GetStorageConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		mockResoruce, ok := mockExtR[extResource.Name]
		assert.True(t, ok)

		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		assert.Equal(t, string(extResource.Data), string(data))
		assert.Equal(t, extResource.Kind, mockResoruce.Kind)
		assert.Equal(t, extResource.Name, mockResoruce.Name)
	}

	// When ocsv1alpha1.StorageConsumerStateReady but ceph resources is empty
	_, err = consumerManager.Create(ctx, "consumer5", "ticket5", resource.MustParse("1G"))
	assert.NoError(t, err)
	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource5.UID),
	}

	server.consumerManager.nameByUID = map[types.UID]string{
		consumerResource5.UID: consumerResource5.Name,
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	assert.Error(t, err)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerStateReady but secret is not ready
	for _, i := range consumerResource.Status.CephResources {
		if i.Kind == "CephClient" {
			cephClient, secret := createCephClientAndSecret(i.Name, server)
			cephClient.Status = &rookCephv1.CephClientStatus{
				Info: map[string]string{
					"secretName": fmt.Sprintf("rook-ceph-client-%s", i.Name),
				},
			}
			assert.NoError(t, client.Delete(ctx, secret)) // deleting all the secrets which were created in previous test
		}
	}

	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource1.UID),
	}

	server.consumerManager.nameByUID = map[types.UID]string{
		consumerResource1.UID: consumerResource1.Name,
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerStateFailed
	_, err = consumerManager.Create(ctx, "consumer1", "ticket1", resource.MustParse("1G"))
	assert.NoError(t, err)
	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource1.UID),
	}

	server.consumerManager.nameByUID = map[types.UID]string{
		consumerResource1.UID: consumerResource1.Name,
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerConfiguring
	_, err = consumerManager.Create(ctx, "consumer2", "ticket2", resource.MustParse("1G"))
	assert.NoError(t, err)
	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource2.UID),
	}

	server.consumerManager.nameByUID = map[types.UID]string{
		consumerResource2.UID: consumerResource2.Name,
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	assert.Error(t, err)
	errCode, _ = status.FromError(err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerDeleting
	_, err = consumerManager.Create(ctx, "consumer3", "ticket3", resource.MustParse("1G"))
	assert.NoError(t, err)
	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource3.UID),
	}

	server.consumerManager.nameByUID = map[types.UID]string{
		consumerResource3.UID: consumerResource3.Name,
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.NotFound)
	assert.Nil(t, storageConRes)

	// When CephClient status is empty
	objects = []runtime.Object{
		&rookCephv1.CephClient{},
	}
	s := runtime.NewScheme()
	assert.NoError(t, v1.AddToScheme(s))
	client = newFakeClient(t, objects...)
	server = &OCSProviderServer{
		client:    client,
		namespace: "openshift-storage",
	}

	for _, i := range consumerResource.Status.CephResources {
		if i.Kind == "CephClient" {
			cephClient, secret := createCephClientAndSecret(i.Name, server)
			assert.NoError(t, client.Create(ctx, cephClient))
			assert.NoError(t, client.Create(ctx, secret))
		}
	}

	exR, err := server.getExternalResources(ctx, consumerResource)
	assert.Error(t, err)
	assert.NotEqual(t, len(mockExtR), len(exR))

	// When CephClient status info is empty

	objects = []runtime.Object{
		&rookCephv1.CephClient{},
	}
	s = runtime.NewScheme()
	assert.NoError(t, v1.AddToScheme(s))
	client = newFakeClient(t, objects...)
	server = &OCSProviderServer{
		client:    client,
		namespace: "openshift-storage",
	}

	for _, i := range consumerResource.Status.CephResources {
		if i.Kind == "CephClient" {
			cephClient, secret := createCephClientAndSecret(i.Name, server)
			cephClient.Status = &rookCephv1.CephClientStatus{
				Info: map[string]string{},
			}

			assert.NoError(t, client.Create(ctx, cephClient))
			assert.NoError(t, client.Create(ctx, secret))
		}
	}

	monCm, monSc = createMonConfigMapAndSecret(server)
	assert.NoError(t, client.Create(ctx, monCm))
	assert.NoError(t, client.Create(ctx, monSc))

	exR, err = server.getExternalResources(ctx, consumerResource)
	assert.Error(t, err)
	assert.NotEqual(t, len(mockExtR), len(exR))
}

func createMonConfigMapAndSecret(server *OCSProviderServer) (*v1.ConfigMap, *v1.Secret) {
	monCm := &v1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{Name: monConfigMap, Namespace: server.namespace},
		Data:       map[string]string{"data": "a=10.99.45.27:6789", "mapping": "{}", "maxMonId": "0"},
	}

	monSc := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: monSecret, Namespace: server.namespace},
		Data: map[string][]byte{
			"fsid": []byte("b88c2d78-9de9-4227-9313-a63f62f78743"),
		},
	}

	return monCm, monSc
}

func createCephClientAndSecret(name string, server *OCSProviderServer) (*rookCephv1.CephClient, *v1.Secret) {
	cephClient := &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: server.namespace,
		},
		Status: &rookCephv1.CephClientStatus{},
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: fmt.Sprintf("rook-ceph-client-%s", name), Namespace: server.namespace},
		Data: map[string][]byte{
			name: []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	return cephClient, secret
}
