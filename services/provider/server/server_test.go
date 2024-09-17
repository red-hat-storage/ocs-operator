package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"testing"

	csiopv1a1 "github.com/ceph/ceph-csi-operator/api/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	pb "github.com/red-hat-storage/ocs-operator/v4/services/provider/pb"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type externalResource struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
	Name string `json:"name"`
}

var serverNamespace = "openshift-storage"
var clusterResourceQuotaSpec = &quotav1.ClusterResourceQuotaSpec{
	Selector: quotav1.ClusterResourceQuotaSelector{
		LabelSelector: &metav1.LabelSelector{
			MatchExpressions: []metav1.LabelSelectorRequirement{
				{
					Key:      "consumer-test",
					Operator: metav1.LabelSelectorOpDoesNotExist,
				},
			},
		},
	},
	Quota: corev1.ResourceQuotaSpec{
		Hard: corev1.ResourceList{"requests.storage": *resource.NewScaledQuantity(
			int64(consumerResource.Spec.StorageQuotaInGiB),
			resource.Giga,
		)},
	},
}

var ocsSubscriptionSpec = &opv1a1.SubscriptionSpec{
	Channel: "1.0",
	Package: "ocs-operator",
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
	"QuotaForConsumer": {
		Name: "QuotaForConsumer",
		Kind: "ClusterResourceQuota",
		Data: map[string]string{
			"QuotaForConsumer": fmt.Sprintf("%+v\n", clusterResourceQuotaSpec),
		},
	},
	"monitor-endpoints": {
		Name: "monitor-endpoints",
		Kind: "CephConnection",
		Data: &csiopv1a1.CephConnectionSpec{Monitors: []string{"10.99.45.27:6789"}},
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

	consumerResource6 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer6",
			UID:       "uid6",
			Namespace: serverNamespace,
		},
		Spec: ocsv1alpha1.StorageConsumerSpec{
			StorageQuotaInGiB: 10240,
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
		consumerResource6,
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

	ocsSubscription := &opv1a1.Subscription{}
	ocsSubscription.Name = "ocs-operator"
	ocsSubscription.Namespace = serverNamespace
	ocsSubscription.Spec = ocsSubscriptionSpec
	assert.NoError(t, client.Create(ctx, ocsSubscription))

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

	// When ocsv1alpha1.StorageConsumerStateReady and quota is set
	req = pb.StorageConfigRequest{
		StorageConsumerUUID: string(consumerResource6.UID),
	}
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		mockResoruce, ok := mockExtR[extResource.Name]
		assert.True(t, ok)

		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		if extResource.Kind == "ClusterResourceQuota" {
			var clusterResourceQuotaSpec quotav1.ClusterResourceQuotaSpec
			err = json.Unmarshal([]byte(extResource.Data), &clusterResourceQuotaSpec)
			assert.NoError(t, err)
			quantity, _ := resource.ParseQuantity("10240G")
			assert.Equal(t, clusterResourceQuotaSpec.Quota.Hard["requests.storage"], quantity)
		} else {
			assert.Equal(t, string(extResource.Data), string(data))
		}

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

func TestOCSProviderServerStorageRequest(t *testing.T) {
	claimNameUnderDeletion := "claim-under-deletion"
	claimResourceUnderDeletion := &ocsv1alpha1.StorageRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getStorageRequestName(string(consumerResource.UID), claimNameUnderDeletion),
			Namespace: serverNamespace,
		},
		Spec: ocsv1alpha1.StorageRequestSpec{
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

	storageRequestManager, err := newStorageRequestManager(client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                client,
		consumerManager:       consumerManager,
		storageRequestManager: storageRequestManager,
		namespace:             serverNamespace,
	}

	req := &pb.FulfillStorageClaimRequest{
		StorageClaimName:    "claim-name",
		StorageConsumerUUID: "consumer-uuid",
		EncryptionMethod:    "vault",
		StorageType:         pb.FulfillStorageClaimRequest_BLOCK,
	}

	// test when consumer not found
	_, err = server.FulfillStorageClaim(ctx, req)
	assert.Error(t, err)

	// test when consumer is found
	req.StorageConsumerUUID = string(consumerResource.UID)
	_, err = server.FulfillStorageClaim(ctx, req)
	assert.NoError(t, err)

	// try to create again with different input
	req.StorageType = pb.FulfillStorageClaimRequest_SHAREDFILE
	_, err = server.FulfillStorageClaim(ctx, req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.AlreadyExists)

	// test when storage class request is under deletion
	req.StorageClaimName = claimNameUnderDeletion
	_, err = server.FulfillStorageClaim(ctx, req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.AlreadyExists)
}

func TestOCSProviderServerRevokeStorageClaim(t *testing.T) {
	claimName := "claim-name"
	claimResource := &ocsv1alpha1.StorageRequest{
		ObjectMeta: metav1.ObjectMeta{
			Name:      getStorageRequestName(string(consumerResource.UID), claimName),
			Namespace: serverNamespace,
		},
		Spec: ocsv1alpha1.StorageRequestSpec{
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

	storageRequestManager, err := newStorageRequestManager(client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                client,
		consumerManager:       consumerManager,
		storageRequestManager: storageRequestManager,
		namespace:             serverNamespace,
	}

	req := &pb.RevokeStorageClaimRequest{
		StorageClaimName:    "claim-name",
		StorageConsumerUUID: string(consumerResource.UID),
	}

	_, err = server.RevokeStorageClaim(ctx, req)
	assert.NoError(t, err)

	// try to delete already deleted resource
	_, err = server.RevokeStorageClaim(ctx, req)
	assert.NoError(t, err)
}

func TestOCSProviderServerGetStorageClaimConfig(t *testing.T) {
	var (
		mockBlockPoolClaimExtR = map[string]*externalResource{
			"ceph-rbd-storageclass": {
				Name: "ceph-rbd",
				Kind: "StorageClass",
				Data: map[string]string{
					"clusterID":                 serverNamespace,
					"pool":                      "cephblockpool",
					"radosnamespace":            "cephradosnamespace",
					"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
					"csi.storage.k8s.io/fstype": "ext4",
					"imageFormat":               "2",
					"csi.storage.k8s.io/provisioner-secret-name":       "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
					"csi.storage.k8s.io/node-stage-secret-name":        "ceph-client-node-8d40b6be71600457b5dec219d2ce2d4c",
					"csi.storage.k8s.io/controller-expand-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
				},
			},
			"ceph-rbd-volumesnapshotclass": {
				Name: "ceph-rbd",
				Kind: "VolumeSnapshotClass",
				Data: map[string]string{
					"csi.storage.k8s.io/snapshotter-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
				},
			},
			"ceph-rbd-volumegroupsnapshotclass": {
				Name: "ceph-rbd",
				Kind: "VolumeGroupSnapshotClass",
				Data: map[string]string{
					"csi.storage.k8s.io/group-snapshotter-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
				},
			},
			"ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c": {
				Name: "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
				Kind: "Secret",
				Data: map[string]string{
					"userID":  "3de200d5c23524a4612bde1fdbeb717e",
					"userKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
				},
			},
			"ceph-client-node-8d40b6be71600457b5dec219d2ce2d4c": {
				Name: "ceph-client-node-8d40b6be71600457b5dec219d2ce2d4c",
				Kind: "Secret",
				Data: map[string]string{
					"userID":  "995e66248ad3e8642de868f461cdd827",
					"userKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
				},
			},
			"ceph-rbd-clientprofile": {
				Name: "ceph-rbd",
				Kind: "ClientProfile",
				Data: &csiopv1a1.ClientProfileSpec{
					Rbd: &csiopv1a1.RbdConfigSpec{
						RadosNamespace: "cephradosnamespace",
					},
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
					"csi.storage.k8s.io/provisioner-secret-name":       "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
					"csi.storage.k8s.io/node-stage-secret-name":        "ceph-client-node-0e8555e6556f70d23a61675af44e880c",
					"csi.storage.k8s.io/controller-expand-secret-name": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
				},
			},
			"cephfs-volumesnapshotclass": {
				Name: "cephfs",
				Kind: "VolumeSnapshotClass",
				Data: map[string]string{
					"csi.storage.k8s.io/snapshotter-secret-name": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
				},
			},

			"cephfs-volumegroupsnapshotclass": {
				Name: "cephfs",
				Kind: "VolumeGroupSnapshotClass",
				Data: map[string]string{
					"csi.storage.k8s.io/group-snapshotter-secret-name": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
				},
			},
			"ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c": {
				Name: "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
				Kind: "Secret",
				Data: map[string]string{
					"adminID":  "4ffcb503ff8044c8699dac415f82d604",
					"adminKey": "AQADw/hhqBOcORAAJY3fKIvte++L/zYhASjYPQ==",
				},
			},
			"ceph-client-node-0e8555e6556f70d23a61675af44e880c": {
				Name: "ceph-client-node-0e8555e6556f70d23a61675af44e880c",
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

			"cephfs-clientprofile": {
				Name: "cephfs",
				Kind: "ClientProfile",
				Data: &csiopv1a1.ClientProfileSpec{
					CephFs: &csiopv1a1.CephFsConfigSpec{
						SubVolumeGroup: "cephFilesystemSubVolumeGroup",
						KernelMountOptions: map[string]string{
							"ms_mode": "legacy",
						},
					},
				},
			},
		}

		blockPoolClaimName     = "block-pool-claim"
		blockPoolClaimResource = &ocsv1alpha1.StorageRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageRequestName(string(consumerResource.UID), blockPoolClaimName),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageRequestStatus{
				CephResources: []*ocsv1alpha1.CephResourcesSpec{
					{
						Name: "cephradosnamespace",
						Kind: "CephBlockPoolRadosNamespace",
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
				Phase: ocsv1alpha1.StorageRequestReady,
			},
		}

		shareFilesystemClaimName      = "shared-filesystem-claim"
		sharedFilesystemClaimResource = &ocsv1alpha1.StorageRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageRequestName(string(consumerResource.UID), shareFilesystemClaimName),
				Namespace: serverNamespace,
			},
			Spec: ocsv1alpha1.StorageRequestSpec{
				Type: "sharedfile",
			},
			Status: ocsv1alpha1.StorageRequestStatus{
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
				Phase: ocsv1alpha1.StorageRequestReady,
			},
		}
		claimInitializing         = "claim-initializing"
		claimCreating             = "claim-creating"
		claimFailed               = "claim-failed"
		claimResourceInitializing = &ocsv1alpha1.StorageRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageRequestName(string(consumerResource.UID), claimInitializing),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageRequestStatus{
				Phase: ocsv1alpha1.StorageRequestInitializing,
			},
		}
		claimResourceCreating = &ocsv1alpha1.StorageRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageRequestName(string(consumerResource.UID), claimCreating),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageRequestStatus{
				Phase: ocsv1alpha1.StorageRequestCreating,
			},
		}
		claimResourceFailed = &ocsv1alpha1.StorageRequest{
			ObjectMeta: metav1.ObjectMeta{
				Name:      getStorageRequestName(string(consumerResource.UID), claimFailed),
				Namespace: serverNamespace,
			},
			Status: ocsv1alpha1.StorageRequestStatus{
				Phase: ocsv1alpha1.StorageRequestFailed,
			},
		}
		storageClusterResourceName = "mock-storage-cluster"
		storageClustersResource    = &ocsv1.StorageCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      storageClusterResourceName,
				Namespace: serverNamespace,
			},
			Spec: ocsv1.StorageClusterSpec{
				AllowRemoteStorageConsumers: true,
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
		storageClustersResource,
	}

	// Create a fake client to mock API calls.
	client := newFakeClient(t, objects...)

	consumerManager, err := newConsumerManager(ctx, client, serverNamespace)
	assert.NoError(t, err)

	storageRequestManager, err := newStorageRequestManager(client, serverNamespace)
	assert.NoError(t, err)

	server := &OCSProviderServer{
		client:                client,
		consumerManager:       consumerManager,
		storageRequestManager: storageRequestManager,
		namespace:             serverNamespace,
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
				"secretName": "ceph-client-node-8d40b6be71600457b5dec219d2ce2d4c",
			},
		},
	}

	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ceph-client-node-8d40b6be71600457b5dec219d2ce2d4c",
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
				"secretName": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
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
				"secretName": "ceph-client-node-0e8555e6556f70d23a61675af44e880c",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ceph-client-node-0e8555e6556f70d23a61675af44e880c",
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
				"secretName": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
			},
		},
	}

	secret = &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
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

	radosNamespace := &rookCephv1.CephBlockPoolRadosNamespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cephradosnamespace",
			Namespace: server.namespace,
		},
		Spec: rookCephv1.CephBlockPoolRadosNamespaceSpec{
			BlockPoolName: "cephblockpool",
		},
	}
	assert.NoError(t, client.Create(ctx, radosNamespace))

	// get the storage class request config for block pool
	req := pb.StorageClaimConfigRequest{
		StorageConsumerUUID: string(consumerResource.UID),
		StorageClaimName:    blockPoolClaimName,
	}
	storageConRes, err := server.GetStorageClaimConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		name := extResource.Name
		if extResource.Kind == "VolumeSnapshotClass" {
			name = fmt.Sprintf("%s-volumesnapshotclass", name)
		} else if extResource.Kind == "StorageClass" {
			name = fmt.Sprintf("%s-storageclass", name)
		} else if extResource.Kind == "VolumeGroupSnapshotClass" {
			name = fmt.Sprintf("%s-volumegroupsnapshotclass", name)
		} else if extResource.Kind == "ClientProfile" {
			name = fmt.Sprintf("%s-clientprofile", name)
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
	req = pb.StorageClaimConfigRequest{
		StorageConsumerUUID: string(consumerResource.UID),
		StorageClaimName:    shareFilesystemClaimName,
	}
	storageConRes, err = server.GetStorageClaimConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

	for i := range storageConRes.ExternalResource {
		extResource := storageConRes.ExternalResource[i]
		name := extResource.Name
		if extResource.Kind == "VolumeSnapshotClass" {
			name = fmt.Sprintf("%s-volumesnapshotclass", name)
		} else if extResource.Kind == "StorageClass" {
			name = fmt.Sprintf("%s-storageclass", name)
		} else if extResource.Kind == "VolumeGroupSnapshotClass" {
			name = fmt.Sprintf("%s-volumegroupsnapshotclass", name)
		} else if extResource.Kind == "ClientProfile" {
			name = fmt.Sprintf("%s-clientprofile", name)
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
	scrNameHash := getStorageRequestHash(string(consumerResource.UID), shareFilesystemClaimName)
	for _, i := range sharedFilesystemClaimResource.Status.CephResources {
		if i.Kind == "CephFilesystemSubVolumeGroup" {
			for cephClientUserType, cephClientName := range i.CephClients {
				cephClient, secret := createCephClientAndSecret(cephClientName, server)
				secret.Name = storageClaimCephCsiSecretName(cephClientUserType, scrNameHash)
				cephClient.Status = &rookCephv1.CephClientStatus{
					Info: map[string]string{
						"secretName": secret.Name,
					},
				}
				assert.NoError(t, client.Delete(ctx, secret))
			}
			break
		}
	}

	storageConRes, err = server.GetStorageClaimConfig(ctx, &req)
	errCode, _ := status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)

	// when claim in in Initializing phase
	req.StorageClaimName = claimInitializing
	storageConRes, err = server.GetStorageClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// when claim in in Creating phase
	req.StorageClaimName = claimCreating
	storageConRes, err = server.GetStorageClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Unavailable)
	assert.Nil(t, storageConRes)

	// when claim in in Failed phase
	req.StorageClaimName = claimFailed
	storageConRes, err = server.GetStorageClaimConfig(ctx, &req)
	errCode, _ = status.FromError(err)
	assert.Error(t, err)
	assert.Equal(t, errCode.Code(), codes.Internal)
	assert.Nil(t, storageConRes)
}
