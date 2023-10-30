package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	pb "github.com/red-hat-storage/ocs-operator/v4/services/provider/pb"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type externalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}

var serverNamespace = "openshift-storage"

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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer",
			UID:       "uid",
			Namespace: serverNamespace,
		},
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer1",
			UID:       "uid1",
			Namespace: serverNamespace,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateFailed,
		},
	}

	consumerResource2 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer2",
			UID:       "uid2",
			Namespace: serverNamespace,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateConfiguring,
		},
	}
	consumerResource3 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer3",
			UID:       "uid3",
			Namespace: serverNamespace,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			State: ocsv1alpha1.StorageConsumerStateDeleting,
		},
	}
	consumerResource4 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer4",
			UID:       "uid4",
			Namespace: serverNamespace,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{},
	}

	consumerResource5 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer5",
			UID:       "uid5",
			Namespace: serverNamespace,
		},
		Status: ocsv1alpha1.StorageConsumerStatus{
			CephResources: []*ocsv1alpha1.CephResourcesSpec{{}},
			State:         ocsv1alpha1.StorageConsumerStateReady,
		},
	}
)

func TestGetExternalResources(t *testing.T) {
	ctx := context.TODO()
	objects := []crClient.Object{
		consumerResource,
		consumerResource1,
		consumerResource2,
		consumerResource3,
		consumerResource4,
		consumerResource5,
	}

	client := newFakeClient(t, objects...)
	consumerManager, err := newConsumerManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	port, _ := strconv.Atoi("9283")
	mgrpod := v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-mgr-test",
			Namespace: serverNamespace,
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
		client:          client,
		namespace:       serverNamespace,
		consumerManager: consumerManager,
	}

	cephClient := &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "995e66248ad3e8642de868f461cdd827",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageCephUserTypeAnnotation: "healthchecker",
				controllers.StorageRequestAnnotation:      "global",
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
	req.StorageConsumerUUID = string(consumerResource5.UID)
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

	req.StorageConsumerUUID = string(consumerResource1.UID)
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerStateFailed
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerConfiguring
	req.StorageConsumerUUID = string(consumerResource2.UID)
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	assert.Error(t, err)
	errCode, _ = status.FromError(err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// When ocsv1alpha1.StorageConsumerDeleting
	req.StorageConsumerUUID = string(consumerResource3.UID)
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.NotFound)
	assert.Nil(t, storageConRes)

	// When CephClient status is empty
	objects = []crClient.Object{
		&rookCephv1.CephClient{},
	}
	s := runtime.NewScheme()
	assert.NoError(t, v1.AddToScheme(s))
	client = newFakeClient(t, objects...)
	server = &OCSProviderServer{
		client:    client,
		namespace: serverNamespace,
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

	objects = []crClient.Object{
		&rookCephv1.CephClient{},
	}
	s = runtime.NewScheme()
	assert.NoError(t, v1.AddToScheme(s))
	client = newFakeClient(t, objects...)
	server = &OCSProviderServer{
		client:    client,
		namespace: serverNamespace,
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

func TestOCSProviderServerStorageClassRequest(t *testing.T) {
	claimNameUnderDeletion := "claim-under-deletion"
	claimResourceUnderDeletion := &ocsv1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getStorageClassRequestName(string(consumerResource.UID), claimNameUnderDeletion),
			Namespace: serverNamespace,
		},
		Spec: ocsv1alpha1.StorageClassRequestSpec{
			Type:             "block",
			EncryptionMethod: "vault",
		},
	}

	ctx := context.TODO()
	objects := []crClient.Object{
		consumerResource,
		claimResourceUnderDeletion,
	}

	// Create a fake client to mock API calls.
	client := newFakeClient(t, objects...)

	consumerManager, err := newConsumerManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	storageClassRequestManager, err := newStorageClassRequestManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                     client,
		consumerManager:            consumerManager,
		storageClassRequestManager: storageClassRequestManager,
		namespace:                  serverNamespace,
	}

	req := &pb.FulfillStorageClassClaimRequest{
		StorageClassClaimName: "claim-name",
		StorageConsumerUUID:   "consumer-uuid",
		EncryptionMethod:      "vault",
		StorageType:           pb.FulfillStorageClassClaimRequest_BLOCKPOOL,
	}

	// test when consumer not found
	_, err = server.FulfillStorageClassClaim(ctx, req)
	assert.Error(t, err)

	// test when consumer is found
	req.StorageConsumerUUID = string(consumerResource.UID)
	_, err = server.FulfillStorageClassClaim(ctx, req)
	assert.NoError(t, err)

	// try to create again with different input
	req.StorageType = pb.FulfillStorageClassClaimRequest_SHAREDFILESYSTEM
	_, err = server.FulfillStorageClassClaim(ctx, req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.AlreadyExists)

	// test when storage class request is under deletion
	req.StorageClassClaimName = claimNameUnderDeletion
	_, err = server.FulfillStorageClassClaim(ctx, req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.AlreadyExists)
}

func TestOCSProviderServerRevokeStorageClassClaim(t *testing.T) {
	claimName := "claim-name"
	claimResource := &ocsv1alpha1.StorageClassRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getStorageClassRequestName(string(consumerResource.UID), claimName),
			Namespace: serverNamespace,
		},
		Spec: ocsv1alpha1.StorageClassRequestSpec{
			Type:             "block",
			EncryptionMethod: "vault",
		},
	}

	ctx := context.TODO()
	objects := []crClient.Object{
		consumerResource,
		claimResource,
	}

	// Create a fake client to mock API calls.
	client := newFakeClient(t, objects...)

	consumerManager, err := newConsumerManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	storageRequestManager, err := newStorageClassRequestManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                     client,
		consumerManager:            consumerManager,
		storageClassRequestManager: storageRequestManager,
		namespace:                  serverNamespace,
	}

	req := &pb.RevokeStorageClassClaimRequest{
		StorageClassClaimName: "claim-name",
		StorageConsumerUUID:   string(consumerResource.UID),
	}

	_, err = server.RevokeStorageClassClaim(ctx, req)
	assert.NoError(t, err)

	// try to delete already deleted resource
	_, err = server.RevokeStorageClassClaim(ctx, req)
	assert.NoError(t, err)
}

func TestOCSProviderServerGetStorageClassClaimConfig(t *testing.T) {
	var (
		mockBlockPoolClaimExtR = map[string]*externalResource{
			"ceph-rbd-storageclass": {
				Name: "ceph-rbd",
				Kind: "StorageClass",
				Data: map[string]string{
					"clusterID":                 serverNamespace,
					"pool":                      "cephblockpool",
					"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
					"csi.storage.k8s.io/fstype": "ext4",
					"imageFormat":               "2",
					"csi.storage.k8s.io/provisioner-secret-name":       "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
					"csi.storage.k8s.io/node-stage-secret-name":        "rook-ceph-client-995e66248ad3e8642de868f461cdd827",
					"csi.storage.k8s.io/controller-expand-secret-name": "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
				},
			},
			"ceph-rbd-volumesnapshotclass": {
				Name: "ceph-rbd",
				Kind: "VolumeSnapshotClass",
				Data: map[string]string{
					"clusterID": serverNamespace,
					"csi.storage.k8s.io/snapshotter-secret-name": "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
				},
			},
			"rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e": {
				Name: "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
				Kind: "Secret",
				Data: map[string]string{
					"userID":  "3de200d5c23524a4612bde1fdbeb717e",
					"userKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
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

		mockShareFilesystemClaimExtR = map[string]*externalResource{
			"cephfs-storageclass": {
				Name: "cephfs",
				Kind: "StorageClass",
				Data: map[string]string{
					"clusterID":          "8d26c7378c1b0ec9c2455d1c3601c4cd",
					"fsName":             "myfs",
					"subvolumegroupname": "cephFilesystemSubVolumeGroup",
					"pool":               "",
					"csi.storage.k8s.io/provisioner-secret-name":       "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
					"csi.storage.k8s.io/node-stage-secret-name":        "rook-ceph-client-1b042fcc8812fe4203689eec38fdfbfa",
					"csi.storage.k8s.io/controller-expand-secret-name": "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
				},
			},
			"cephfs-volumesnapshotclass": {
				Name: "cephfs",
				Kind: "VolumeSnapshotClass",
				Data: map[string]string{
					"clusterID": "8d26c7378c1b0ec9c2455d1c3601c4cd",
					"csi.storage.k8s.io/snapshotter-secret-name": "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
				},
			},
			"rook-ceph-client-4ffcb503ff8044c8699dac415f82d604": {
				Name: "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
				Kind: "Secret",
				Data: map[string]string{
					"adminID":  "4ffcb503ff8044c8699dac415f82d604",
					"adminKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
				},
			},
			"rook-ceph-client-1b042fcc8812fe4203689eec38fdfbfa": {
				Name: "rook-ceph-client-1b042fcc8812fe4203689eec38fdfbfa",
				Kind: "Secret",
				Data: map[string]string{
					"adminID":  "1b042fcc8812fe4203689eec38fdfbfa",
					"adminKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
				},
			},

			"cephFilesystemSubVolumeGroup": {
				Name: "cephFilesystemSubVolumeGroup",
				Kind: "CephFilesystemSubVolumeGroup",
				Data: map[string]string{
					"filesystemName": "myfs",
				},
			},
		}

		blockPoolClaimName     = "block-pool-claim"
		blockPoolClaimResource = &ocsv1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageClassRequestName(string(consumerResource.UID), blockPoolClaimName),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageClassRequestStatus{
				CephResources: []*ocsv1alpha1.CephResourcesSpec{
					{
						Name: "cephblockpool",
						Kind: "CephBlockPool",
						CephClients: map[string]string{
							"node":        "995e66248ad3e8642de868f461cdd827",
							"provisioner": "3de200d5c23524a4612bde1fdbeb717e",
						},
					},
					{
						Name: "3de200d5c23524a4612bde1fdbeb717e",
						Kind: "CephClient",
					},
					{
						Name: "995e66248ad3e8642de868f461cdd827",
						Kind: "CephClient",
					},
				},
				Phase: ocsv1alpha1.StorageClassRequestReady,
			},
		}

		shareFilesystemClaimName      = "shared-filesystem-claim"
		sharedFilesystemClaimResource = &ocsv1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageClassRequestName(string(consumerResource.UID), shareFilesystemClaimName),
				Namespace: serverNamespace,
			},
			Spec: ocsv1alpha1.StorageClassRequestSpec{
				Type: "sharedfilesystem",
			},
			Status: ocsv1alpha1.StorageClassRequestStatus{
				CephResources: []*ocsv1alpha1.CephResourcesSpec{
					{
						Name: "cephFilesystemSubVolumeGroup",
						Kind: "CephFilesystemSubVolumeGroup",
						CephClients: map[string]string{
							"node":        "1b042fcc8812fe4203689eec38fdfbfa",
							"provisioner": "4ffcb503ff8044c8699dac415f82d604",
						},
					},
					{
						Name: "4ffcb503ff8044c8699dac415f82d604",
						Kind: "CephClient",
					},
					{
						Name: "1b042fcc8812fe4203689eec38fdfbfa",
						Kind: "CephClient",
					},
				},
				Phase: ocsv1alpha1.StorageClassRequestReady,
			},
		}
		claimInitializing         = "claim-initializing"
		claimCreating             = "claim-creating"
		claimFailed               = "claim-failed"
		claimResourceInitializing = &ocsv1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageClassRequestName(string(consumerResource.UID), claimInitializing),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageClassRequestStatus{
				Phase: ocsv1alpha1.StorageClassRequestInitializing,
			},
		}
		claimResourceCreating = &ocsv1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageClassRequestName(string(consumerResource.UID), claimCreating),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageClassRequestStatus{
				Phase: ocsv1alpha1.StorageClassRequestCreating,
			},
		}
		claimResourceFailed = &ocsv1alpha1.StorageClassRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageClassRequestName(string(consumerResource.UID), claimFailed),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageClassRequestStatus{
				Phase: ocsv1alpha1.StorageClassRequestFailed,
			},
		}
	)

	ctx := context.TODO()
	objects := []crClient.Object{
		consumerResource,
		blockPoolClaimResource,
		sharedFilesystemClaimResource,
		claimResourceInitializing,
		claimResourceCreating,
		claimResourceFailed,
	}

	// Create a fake client to mock API calls.
	client := newFakeClient(t, objects...)

	consumerManager, err := newConsumerManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	storageClassRequestManager, err := newStorageClassRequestManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                     client,
		consumerManager:            consumerManager,
		storageClassRequestManager: storageClassRequestManager,
		namespace:                  serverNamespace,
	}

	cephClient := &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "995e66248ad3e8642de868f461cdd827",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageRequestAnnotation:      "rbd",
				controllers.StorageCephUserTypeAnnotation: "node",
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
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-client-995e66248ad3e8642de868f461cdd827",
			Namespace: server.namespace,
		},
		Data: map[string][]byte{
			"995e66248ad3e8642de868f461cdd827": []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	assert.NoError(t, client.Create(ctx, cephClient))
	assert.NoError(t, client.Create(ctx, secret))

	cephClient = &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "3de200d5c23524a4612bde1fdbeb717e",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageRequestAnnotation:      "rbd",
				controllers.StorageCephUserTypeAnnotation: "provisioner",
				controllers.StorageConsumerAnnotation:     "consumer",
			},
		},
		Status: &rookCephv1.CephClientStatus{
			Info: map[string]string{
				"secretName": "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-client-3de200d5c23524a4612bde1fdbeb717e",
			Namespace: server.namespace,
		},
		Data: map[string][]byte{
			"3de200d5c23524a4612bde1fdbeb717e": []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	assert.NoError(t, client.Create(ctx, cephClient))
	assert.NoError(t, client.Create(ctx, secret))

	cephClient = &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "1b042fcc8812fe4203689eec38fdfbfa",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageRequestAnnotation:      "cephfs",
				controllers.StorageCephUserTypeAnnotation: "node",
				controllers.StorageConsumerAnnotation:     "consumer",
			},
		},
		Status: &rookCephv1.CephClientStatus{
			Info: map[string]string{
				"secretName": "rook-ceph-client-1b042fcc8812fe4203689eec38fdfbfa",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-client-1b042fcc8812fe4203689eec38fdfbfa",
			Namespace: server.namespace,
		},
		Data: map[string][]byte{
			"1b042fcc8812fe4203689eec38fdfbfa": []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	assert.NoError(t, client.Create(ctx, cephClient))
	assert.NoError(t, client.Create(ctx, secret))

	cephClient = &rookCephv1.CephClient{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "4ffcb503ff8044c8699dac415f82d604",
			Namespace: server.namespace,
			Annotations: map[string]string{
				controllers.StorageRequestAnnotation:      "cephfs",
				controllers.StorageCephUserTypeAnnotation: "provisioner",
				controllers.StorageConsumerAnnotation:     "consumer",
			},
		},
		Status: &rookCephv1.CephClientStatus{
			Info: map[string]string{
				"secretName": "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rook-ceph-client-4ffcb503ff8044c8699dac415f82d604",
			Namespace: server.namespace,
		},
		Data: map[string][]byte{
			"4ffcb503ff8044c8699dac415f82d604": []byte("AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ=="),
		},
	}

	assert.NoError(t, client.Create(ctx, cephClient))
	assert.NoError(t, client.Create(ctx, secret))

	subVolGroup := &rookCephv1.CephFilesystemSubVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cephFilesystemSubVolumeGroup",
			Namespace: server.namespace,
		},
		Spec: rookCephv1.CephFilesystemSubVolumeGroupSpec{
			FilesystemName: "myfs",
		},
	}
	assert.NoError(t, client.Create(ctx, subVolGroup))

	// get the storage class request config for block pool
	req := pb.StorageClassClaimConfigRequest{
		StorageConsumerUUID:   string(consumerResource.UID),
		StorageClassClaimName: blockPoolClaimName,
	}
	storageConRes, err := server.GetStorageClassClaimConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		name := extResource.Name
		if extResource.Kind == "VolumeSnapshotClass" {
			name = fmt.Sprintf("%s-volumesnapshotclass", name)
		} else if extResource.Kind == "StorageClass" {
			name = fmt.Sprintf("%s-storageclass", name)
		}
		mockResoruce, ok := mockBlockPoolClaimExtR[name]
		assert.True(t, ok)

		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		assert.Equal(t, string(extResource.Data), string(data))
		assert.Equal(t, extResource.Kind, mockResoruce.Kind)
		assert.Equal(t, extResource.Name, mockResoruce.Name)
	}

	// get the storage class request config for share filesystem
	req = pb.StorageClassClaimConfigRequest{
		StorageConsumerUUID:   string(consumerResource.UID),
		StorageClassClaimName: shareFilesystemClaimName,
	}
	storageConRes, err = server.GetStorageClassClaimConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		name := extResource.Name
		if extResource.Kind == "VolumeSnapshotClass" {
			name = fmt.Sprintf("%s-volumesnapshotclass", name)
		} else if extResource.Kind == "StorageClass" {
			name = fmt.Sprintf("%s-storageclass", name)
		}
		mockResoruce, ok := mockShareFilesystemClaimExtR[name]
		assert.True(t, ok)

		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		assert.Equal(t, string(extResource.Data), string(data))
		assert.Equal(t, extResource.Kind, mockResoruce.Kind)
		assert.Equal(t, extResource.Name, mockResoruce.Name)
	}

	// When ceph resources is empty
	for _, i := range sharedFilesystemClaimResource.Status.CephResources {
		if i.Kind == "CephClient" {
			cephClient, secret := createCephClientAndSecret(i.Name, server)
			cephClient.Status = &rookCephv1.CephClientStatus{
				Info: map[string]string{
					"secretName": fmt.Sprintf("rook-ceph-client-%s", i.Name),
				},
			}
			assert.NoError(t, client.Delete(ctx, secret))
		}
	}

	storageConRes, err = server.GetStorageClassClaimConfig(ctx, &req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// when claim in in Initializing phase
	req.StorageClassClaimName = claimInitializing
	storageConRes, err = server.GetStorageClassClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// when claim in in Creating phase
	req.StorageClassClaimName = claimCreating
	storageConRes, err = server.GetStorageClassClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// when claim in in Failed phase
	req.StorageClassClaimName = claimFailed
	storageConRes, err = server.GetStorageClassClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)
}
