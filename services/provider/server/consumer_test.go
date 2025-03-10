package server

import (
	"context"
	"testing"

	nbapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	routev1 "github.com/openshift/api/route/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const (
	testNamespace = "test"
)

var (
	consumer1 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer1",
			Namespace: testNamespace,
			Annotations: map[string]string{
				TicketAnnotation: "ticket1",
			},
			UID: "uid1",
		},
		Spec: ocsv1alpha1.StorageConsumerSpec{
			Enable: true,
		},
	}

	consumer2 = &ocsv1alpha1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "consumer2",
			Namespace: testNamespace,
			Annotations: map[string]string{
				TicketAnnotation: "ticket2",
			},
			UID: "uid2",
		},
	}
)

func newFakeClient(t *testing.T, obj ...client.Object) client.Client {
	scheme := runtime.NewScheme()

	err := api.AddToScheme(scheme)
	assert.NoError(t, err, "unable to build scheme")

	err = corev1.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add corev1 scheme")

	err = ocsv1alpha1.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add ocsv1alpha1 scheme")

	err = rookCephv1.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add rookCephv1 scheme")

	err = opv1a1.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add opv1a1 scheme")

	err = nbapis.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add opv1a1 scheme")

	err = routev1.AddToScheme(scheme)
	assert.NoError(t, err, "failed to add nbapis scheme")
	return fake.NewClientBuilder().
		WithScheme(scheme).
		WithObjects(obj...).
		WithStatusSubresource(obj...).Build()
}

func TestNewConsumerManager(t *testing.T) {
	ctx := context.TODO()
	obj := []client.Object{}

	// Test NewConsumerManager with no StorageConsumer resources
	client := newFakeClient(t)
	consumerManager, err := newConsumerManager(ctx, client, testNamespace)
	assert.Nil(t, err)
	assert.Equal(t, 0, len(consumerManager.nameByTicket))
	assert.Equal(t, 0, len(consumerManager.nameByUID))

	// Test NewConsumerManager when StorageConsumer resources are already available
	obj = append(obj, consumer1, consumer2)
	client = newFakeClient(t, obj...)
	consumerManager, err = newConsumerManager(ctx, client, testNamespace)
	assert.Nil(t, err)
	assert.Equal(t, 2, len(consumerManager.nameByTicket))
	assert.Equal(t, "consumer1", consumerManager.nameByTicket["ticket1"])
	assert.Equal(t, "consumer2", consumerManager.nameByTicket["ticket2"])
	assert.Equal(t, 2, len(consumerManager.nameByUID))
	assert.Equal(t, "consumer1", consumerManager.nameByUID["uid1"])
	assert.Equal(t, "consumer2", consumerManager.nameByUID["uid2"])
}

func TestCreateStorageConsumer(t *testing.T) {
	ctx := context.TODO()
	obj := []client.Object{}

	obj = append(obj, consumer1)
	client := newFakeClient(t, obj...)
	consumerManager, err := newConsumerManager(ctx, client, testNamespace)
	assert.NoError(t, err)

	// Create consumer should fail if consumer already exists
	req := providerClient.NewOnboardConsumerRequest().
		SetConsumerName("consumer1").
		SetOnboardingTicket("ticket1")
	_, err = consumerManager.Create(ctx, req, 0)
	assert.Error(t, err)

	// Create consumer should fail if ticket is already used
	req = providerClient.NewOnboardConsumerRequest().
		SetConsumerName("consumer3").
		SetOnboardingTicket("ticket1")
	_, err = consumerManager.Create(ctx, req, 0)
	assert.Error(t, err)

	// Create consumer successfully. (Can't validate the UID because fake client does not add UID)
	assert.Equal(t, 1, len(consumerManager.nameByTicket))
	req = providerClient.NewOnboardConsumerRequest().
		SetConsumerName("consumer2").
		SetOnboardingTicket("ticket2")
	_, err = consumerManager.Create(ctx, req, 0)
	assert.NoError(t, err)
	assert.Equal(t, 2, len(consumerManager.nameByTicket))
	assert.Equal(t, "consumer1", consumerManager.nameByTicket["ticket1"])
	assert.Equal(t, "consumer2", consumerManager.nameByTicket["ticket2"])
	consumerObject, err := consumerManager.GetByName(ctx, "consumer1")
	assert.NoError(t, err)
	assert.Equal(t, consumerObject.Spec.StorageQuotaInGiB, 0)

	version := "4.15.1"
	name := "consumer3"
	storageQuotaInGiB := 1024
	req = providerClient.NewOnboardConsumerRequest().
		SetConsumerName("consumer3").
		SetOnboardingTicket("ticket3").
		SetClientOperatorVersion(version)
	_, err = consumerManager.Create(ctx, req, 1024)
	assert.NoError(t, err)
	consumerObject, err = consumerManager.GetByName(ctx, name)
	assert.NoError(t, err)
	assert.Equal(t, consumerObject.Status.Client.OperatorVersion, version)
	assert.Equal(t, consumerObject.Spec.StorageQuotaInGiB, storageQuotaInGiB)
}

func TestDeleteStorageConsumer(t *testing.T) {
	ctx := context.TODO()
	obj := []client.Object{}

	obj = append(obj, consumer1)
	client := newFakeClient(t, obj...)
	consumerManager, err := newConsumerManager(ctx, client, testNamespace)
	assert.NoError(t, err)

	// Delete consumer should ignore error if consumer is not found
	err = consumerManager.Delete(ctx, "invalid-uid")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(consumerManager.nameByUID))

	// Delete consumer removes any stale references in the nameByUID map
	consumerManager.nameByUID["stale-uid"] = "stale-consumer"
	assert.Equal(t, 2, len(consumerManager.nameByUID))
	err = consumerManager.Delete(ctx, "stale-uid")
	assert.NoError(t, err)
	assert.Equal(t, 1, len(consumerManager.nameByUID))

	// Delete consumer successfully deletes an existing consumer
	err = consumerManager.Delete(ctx, "uid1")
	assert.NoError(t, err)
	assert.Equal(t, 0, len(consumerManager.nameByUID))
}

func TestGetStorageConsumer(t *testing.T) {
	ctx := context.TODO()
	obj := []client.Object{}

	obj = append(obj, consumer1)
	client := newFakeClient(t, obj...)
	consumerManager, err := newConsumerManager(ctx, client, testNamespace)
	assert.NoError(t, err)

	// Get storageConsumer should fail for invalid UID
	_, err = consumerManager.Get(ctx, "invalid-uid")
	assert.Error(t, err)

	// Get storageConsumer should succeed for valid UID
	consumer, err := consumerManager.Get(ctx, "uid1")
	assert.NoError(t, err)
	assert.Equal(t, "consumer1", consumer.Name)
}

func TestUpdateConsumerStatus(t *testing.T) {
	ctx := context.TODO()
	obj := []client.Object{}

	consumer := &ocsv1alpha1.StorageConsumer{}
	consumer1.DeepCopyInto(consumer)

	// status should be preserved after update
	consumer.Status.State = ocsv1alpha1.StorageConsumerStateReady

	obj = append(obj, consumer)
	client := newFakeClient(t, obj...)
	consumerManager, err := newConsumerManager(ctx, client,
		testNamespace)
	assert.NoError(t, err)

	// with fields
	fields := providerClient.NewStorageClientStatus().
		SetPlatformVersion("1.0.0").
		SetOperatorVersion("1.0.0")
	err = consumerManager.UpdateConsumerStatus(ctx, "uid1", fields)
	assert.NoError(t, err)

	c1, err := consumerManager.Get(ctx, "uid1")
	assert.NoError(t, err)
	assert.NotEmpty(t, c1.Status.LastHeartbeat)
	assert.Equal(t, fields.GetPlatformVersion(), c1.Status.Client.PlatformVersion)
	assert.Equal(t, fields.GetOperatorVersion(), c1.Status.Client.OperatorVersion)
	assert.Equal(t, c1.Status.State, ocsv1alpha1.StorageConsumerStateReady)
}
