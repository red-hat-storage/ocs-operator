package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	"k8s.io/utils/ptr"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	csiopv1a1 "github.com/ceph/ceph-csi-operator/api/v1alpha1"
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	crClient "sigs.k8s.io/controller-runtime/pkg/client"
)

type externalResource struct {
	Kind        string            `json:"kind"`
	Data        any               `json:"data"`
	Name        string            `json:"name"`
	Labels      map[string]string `json:"labels"`
	Annotations map[string]string `json:"annotations"`
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
		Hard: corev1.ResourceList{"requests.storage": *resource.NewQuantity(
			int64(consumerResource.Spec.StorageQuotaInGiB)*oneGibInBytes,
			resource.BinarySI,
		)},
	},
}

var ocsSubscriptionSpec = &opv1a1.SubscriptionSpec{
	Channel: "1.0",
	Package: "ocs-operator",
}
var noobaaSpec = &nbv1.NooBaaSpec{
	JoinSecret: &v1.SecretReference{
		Name: "noobaa-remote-join-secret",
	},
}

var joinSecret = map[string]string{
	"auth_token": "authToken",
	"mgmt_addr":  "noobaaMgmtAddress",
}

var nbProviderStatus = &nbv1.NooBaaStatus{
	Phase: nbv1.SystemPhaseReady,
	Services: &nbv1.ServicesStatus{
		ServiceS3: nbv1.ServiceStatus{
			NodePorts: []string{"noobaaS3Endpoint"},
		},
	},
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
	"monitoring-endpoint": {
		Name: "monitoring-endpoint",
		Kind: "CephCluster",
		Data: map[string]string{
			"MonitoringEndpoint": "10.105.164.231",
			"MonitoringPort":     "9283",
		},
	},

	"QuotaForConsumer": {
		Name: "QuotaForConsumer",
		Kind: "ClusterResourceQuota",
		Data: map[string]string{
			"QuotaForConsumer": fmt.Sprintf("%+v\n", clusterResourceQuotaSpec),
		},
	},
	"noobaa-remote-join-secret": {
		Name: "noobaa-remote-join-secret",
		Kind: "Secret",
		Data: joinSecret,
	},
	"noobaa-remote": {
		Name: "noobaa-remote",
		Kind: "Noobaa",
		Data: noobaaSpec,
	},
	"monitor-endpoints": {
		Name: "monitor-endpoints",
		Kind: "CephConnection",
		Data: &csiopv1a1.CephConnectionSpec{Monitors: []string{"10.99.45.27:3300"}},
	},
	"s3-endpoint-proxy": {
		Name: "s3-endpoint-proxy",
		Kind: "Service",
		Data: v1.ServiceSpec{Type: corev1.ServiceTypeExternalName, ExternalName: "noobaaS3Endpoint", Ports: []v1.ServicePort{{Port: 443, TargetPort: intstr.FromInt(443)}}},
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
			CephResources: []*ocsv1alpha1.CephResourcesSpec{},
			State:         ocsv1alpha1.StorageConsumerStateReady,
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

	noobaaRemoteJoinSecretConsumer := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noobaa-account-consumer", Namespace: server.namespace},
		Data: map[string][]byte{
			"auth_token": []byte("authToken"),
		},
	}

	noobaaRemoteJoinSecretConsumer6 := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "noobaa-account-consumer6", Namespace: server.namespace},
		Data: map[string][]byte{
			"auth_token": []byte("authToken"),
		},
	}

	noobaaMgmtRoute := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{Name: "noobaa-mgmt", Namespace: server.namespace},
		Status: routev1.RouteStatus{
			Ingress: []routev1.RouteIngress{{Host: "noobaaMgmtAddress"}},
		},
	}

	assert.NoError(t, client.Create(ctx, noobaaRemoteJoinSecretConsumer))
	assert.NoError(t, client.Create(ctx, noobaaRemoteJoinSecretConsumer6))
	assert.NoError(t, client.Create(ctx, noobaaMgmtRoute))

	monCm, monSc := createMonConfigMapAndSecret(server)
	assert.NoError(t, client.Create(ctx, monCm))
	assert.NoError(t, client.Create(ctx, monSc))

	ocsSubscription := &opv1a1.Subscription{}
	ocsSubscription.Name = "ocs-operator"
	ocsSubscription.Namespace = serverNamespace
	ocsSubscription.Spec = ocsSubscriptionSpec
	assert.NoError(t, client.Create(ctx, ocsSubscription))

	noobaa := &nbv1.NooBaa{}
	noobaa.Name = "noobaa"
	noobaa.Namespace = serverNamespace
	noobaa.Status = *nbProviderStatus
	assert.NoError(t, client.Create(ctx, noobaa))

	storageCluster := &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			AllowRemoteStorageConsumers: true,
		},
	}
	storageCluster.Name = "test-storagecluster"
	storageCluster.Namespace = serverNamespace
	assert.NoError(t, client.Create(ctx, storageCluster))

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

		if extResource.Kind == "Noobaa" {
			var extNoobaaSpec, mockNoobaaSpec nbv1.NooBaaSpec
			err = json.Unmarshal(extResource.Data, &extNoobaaSpec)
			assert.NoError(t, err)
			data, err := json.Marshal(mockResoruce.Data)
			assert.NoError(t, err)
			err = json.Unmarshal(data, &mockNoobaaSpec)
			assert.NoError(t, err)
			assert.Equal(t, extNoobaaSpec.JoinSecret, mockNoobaaSpec.JoinSecret)
		} else {
			data, err := json.Marshal(mockResoruce.Data)
			assert.NoError(t, err)
			assert.Equal(t, string(extResource.Data), string(data))

		}
		assert.Equal(t, extResource.GetLabels(), mockResoruce.Labels)
		assert.Equal(t, extResource.GetAnnotations(), mockResoruce.Annotations)
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

		if extResource.Kind == "ClusterResourceQuota" {
			var clusterResourceQuotaSpec quotav1.ClusterResourceQuotaSpec
			err = json.Unmarshal([]byte(extResource.Data), &clusterResourceQuotaSpec)
			assert.NoError(t, err)
			expected := resource.NewQuantity(int64(10240)*oneGibInBytes, resource.BinarySI)
			actual := clusterResourceQuotaSpec.Quota.Hard["requests.storage"]
			assert.Equal(t, actual.Value(), expected.Value())
		} else if extResource.Kind == "Noobaa" {
			var extNoobaaSpec, mockNoobaaSpec nbv1.NooBaaSpec
			err = json.Unmarshal(extResource.Data, &extNoobaaSpec)
			assert.NoError(t, err)
			data, err := json.Marshal(mockResoruce.Data)
			assert.NoError(t, err)
			err = json.Unmarshal(data, &mockNoobaaSpec)
			assert.NoError(t, err)
			assert.Equal(t, mockNoobaaSpec.JoinSecret, extNoobaaSpec.JoinSecret)
		} else {
			data, err := json.Marshal(mockResoruce.Data)
			assert.NoError(t, err)
			assert.Equal(t, string(extResource.Data), string(data))
		}
		assert.Equal(t, extResource.GetLabels(), mockResoruce.Labels)
		assert.Equal(t, extResource.GetAnnotations(), mockResoruce.Annotations)
		assert.Equal(t, extResource.Kind, mockResoruce.Kind)
		assert.Equal(t, extResource.Name, mockResoruce.Name)
	}

	// When ocsv1alpha1.StorageConsumerStateReady but ceph resources is empty
	req.StorageConsumerUUID = string(consumerResource5.UID)
	storageConRes, err = server.GetStorageConfig(ctx, &req)
	assert.NoError(t, err)
	assert.NotNil(t, storageConRes)

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

func TestReplaceMsgr1PortWithMsgr2(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected []string
	}{
		{
			name:     "no msgr1 port",
			input:    []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "all msgr1 ports",
			input:    []string{"192.168.1.1:6789", "192.168.1.2:6789", "192.168.1.3:6789"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "mixed ports",
			input:    []string{"192.168.1.1:6789", "192.168.1.2:3300", "192.168.1.3:6789"},
			expected: []string{"192.168.1.1:3300", "192.168.1.2:3300", "192.168.1.3:3300"},
		},
		{
			name:     "empty slice",
			input:    []string{},
			expected: []string{},
		},
		{
			name:     "no port in IP",
			input:    []string{"192.168.1.1", "192.168.1.2:6789", "192.168.1.2:6789"},
			expected: []string{"192.168.1.1", "192.168.1.2:3300", "192.168.1.2:3300"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Make a copy of the input slice to avoid modifying the original
			inputCopy := make([]string, len(tt.input))
			copy(inputCopy, tt.input)
			replaceMsgr1PortWithMsgr2(inputCopy)
			if !reflect.DeepEqual(inputCopy, tt.expected) {
				t.Errorf("replaceMsgr1PortWithMsgr2() = %v, expected %v", inputCopy, tt.expected)
			}
		})
	}
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
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "854666c7477123fb05f20bf615e69a46",
				},
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
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "854666c7477123fb05f20bf615e69a46",
				},
				Data: map[string]string{
					"csi.storage.k8s.io/snapshotter-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
				},
			},
			"groupsnapclass": {
				Name: "block-pool-claim-groupsnapclass",
				Kind: "VolumeGroupSnapshotClass",
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "854666c7477123fb05f20bf615e69a46",
				},
				Data: map[string]string{
					"csi.storage.k8s.io/group-snapshotter-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
					"clusterID": "ddc427cbb7c588e1c404cb05cfe6d07b",
					"pool":      "cephblockpool",
				},
			},
			"rbd-volumereplicationclass-1625360775": {
				Name: "rbd-volumereplicationclass-1625360775",
				Kind: "VolumeReplicationClass",
				Labels: map[string]string{
					"ramendr.openshift.io/replicationid":    "a06c234d29cb35b6fe45fb3d7c8dd8a6",
					"ramendr.openshift.io/storageid":        "854666c7477123fb05f20bf615e69a46",
					"ramendr.openshift.io/maintenancemodes": "Failover",
				},
				Annotations: map[string]string{
					"replication.storage.openshift.io/is-default-class": "true",
				},
				Data: &replicationv1alpha1.VolumeReplicationClassSpec{
					Parameters: map[string]string{
						"replication.storage.openshift.io/replication-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
						"mirroringMode":      "snapshot",
						"schedulingInterval": "5m",
						"clusterID":          "ddc427cbb7c588e1c404cb05cfe6d07b",
					},
					Provisioner: util.RbdDriverName,
				},
			},
			"rbd-flatten-volumereplicationclass-1625360775": {
				Name: "rbd-flatten-volumereplicationclass-1625360775",
				Kind: "VolumeReplicationClass",
				Labels: map[string]string{
					"replication.storage.openshift.io/flatten-mode": "force",
					"ramendr.openshift.io/replicationid":            "a06c234d29cb35b6fe45fb3d7c8dd8a6",
					"ramendr.openshift.io/storageid":                "854666c7477123fb05f20bf615e69a46",
					"ramendr.openshift.io/maintenancemodes":         "Failover",
				},
				Data: &replicationv1alpha1.VolumeReplicationClassSpec{
					Parameters: map[string]string{
						"replication.storage.openshift.io/replication-secret-name": "ceph-client-provisioner-8d40b6be71600457b5dec219d2ce2d4c",
						"mirroringMode":      "snapshot",
						"flattenMode":        "force",
						"schedulingInterval": "5m",
						"clusterID":          "ddc427cbb7c588e1c404cb05cfe6d07b",
					},
					Provisioner: util.RbdDriverName,
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
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "5b53ada3302d6e0d1025a7948ce45ba5",
				},
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
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "5b53ada3302d6e0d1025a7948ce45ba5",
				},
				Data: map[string]string{
					"csi.storage.k8s.io/snapshotter-secret-name": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
				},
			},
			"groupsnapclass": {
				Name: "shared-filesystem-claim-groupsnapclass",
				Kind: "VolumeGroupSnapshotClass",
				Labels: map[string]string{
					"ramendr.openshift.io/storageid": "5b53ada3302d6e0d1025a7948ce45ba5",
				},
				Data: map[string]string{
					"csi.storage.k8s.io/group-snapshotter-secret-name": "ceph-client-provisioner-0e8555e6556f70d23a61675af44e880c",
					"clusterID": "2bebeaaed3a6b5df66f79f8129a11585",
					"fsName":    "myfs",
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
							"ms_mode": "prefer-crc",
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
		cephCluster = &rookCephv1.CephCluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "mock-storage-cluster-cephcluster",
				Namespace: serverNamespace,
			},
			Status: rookCephv1.ClusterStatus{
				CephStatus: &rookCephv1.CephStatus{
					FSID: "my-fsid",
				},
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
		cephCluster,
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
			Mirroring: &rookCephv1.RadosNamespaceMirroring{
				Mode:            "pool",
				RemoteNamespace: ptr.To("peer-remote"),
			},
		},
	}
	assert.NoError(t, client.Create(ctx, radosNamespace))

	cephBlockPool := &rookCephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cephblockpool",
			Namespace: server.namespace,
			Annotations: map[string]string{
				"ocs.openshift.io/mirroring-target-id": "3",
			},
		},
		Spec: rookCephv1.NamedBlockPoolSpec{
			PoolSpec: rookCephv1.PoolSpec{
				Mirroring: rookCephv1.MirroringSpec{
					Enabled: false,
					Peers: &rookCephv1.MirroringPeerSpec{
						SecretNames: []string{
							"mirroring-token",
						},
					},
				},
			},
		},
		Status: &rookCephv1.CephBlockPoolStatus{
			PoolID: 1,
		},
	}
	assert.NoError(t, client.Create(ctx, cephBlockPool))

	mirroringToken := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mirroring-token",
			Namespace: server.namespace,
		},
		Data: map[string][]byte{
			"token": []byte("eyJmc2lkIjoiZjk1NGYwNGItM2M1Yi00MWMxLWFkODUtNjEzNzA3ZDBjZDllIiwiY2xpZW50X2lkIjoicmJkLW1pcnJvci1wZWVyIiwia2V5IjoiQVFDYUZJNW5ZRVJrR0JBQWQzLzVQS1V3RFVuTXkxbHBMSEh2ZXc9PSIsIm1vbl9ob3N0IjoidjI6MTAuMC4xMzUuMTY5OjMzMDAvMCx2MjoxMC4wLjE1Ny4xNTY6MzMwMC8wLHYyOjEwLjAuMTcyLjExMjozMzAwLzAiLCJuYW1lc3BhY2UiOiJvcGVuc2hpZnQtc3RvcmFnZSJ9"),
		},
	}
	assert.NoError(t, client.Create(ctx, mirroringToken))

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
			name = "groupsnapclass"
		} else if extResource.Kind == "ClientProfile" {
			name = fmt.Sprintf("%s-clientprofile", name)
		}
		mockResoruce, ok := mockBlockPoolClaimExtR[name]
		assert.True(t, ok)

		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		assert.Equal(t, string(extResource.Data), string(data))
		assert.Equal(t, extResource.GetLabels(), mockResoruce.Labels)
		assert.Equal(t, extResource.GetAnnotations(), mockResoruce.Annotations)
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
			name = "groupsnapclass"
		} else if extResource.Kind == "VolumeReplicationClass" {
			name = fmt.Sprintf("%s-volumereplicationclass", name)
		} else if extResource.Kind == "ClientProfile" {
			name = fmt.Sprintf("%s-clientprofile", name)
		}
		mockResoruce, ok := mockShareFilesystemClaimExtR[name]
		assert.True(t, ok)
		data, err := json.Marshal(mockResoruce.Data)
		assert.NoError(t, err)
		assert.Equal(t, string(extResource.Data), string(data))
		assert.Equal(t, extResource.GetLabels(), mockResoruce.Labels)
		assert.Equal(t, extResource.GetAnnotations(), mockResoruce.Annotations)
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
