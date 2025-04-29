package server

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"

	csiopv1a1 "github.com/ceph/ceph-csi-operator/api/v1alpha1"
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
	//TODO: rework once all the PRs are merged
	t.Skip("deferred till FF")
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

	storageCluster := &ocsv1.StorageCluster{}
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
