package server

import (
	"context"
	"fmt"
	"sigs.k8s.io/controller-runtime/pkg/cache"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type ocsConsumerManager struct {
	client    client.Client
	namespace string
}

func newConsumerManager(ctx context.Context, cl client.Client, cache cache.Cache, namespace string) (*ocsConsumerManager, error) {

	if err := cache.IndexField(
		ctx,
		&ocsv1alpha1.StorageConsumer{},
		util.ObjectUidIndexName,
		util.ObjectUidIndexFieldFunc,
	); err != nil {
		return nil, fmt.Errorf("failed to set up FieldIndexer on StorageConsumer for uid: %v", err)
	}

	return &ocsConsumerManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

// EnableStorageConsumer enables storageConsumer resource
func (c *ocsConsumerManager) EnableStorageConsumer(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	clientInfo ifaces.StorageClientOnboarding,
) (string, error) {

	// k8s spec and status are two different endpoints and we need to update them separately
	fillStorageClientInfo(&consumer.Status, clientInfo)
	if err := c.client.Status().Update(ctx, consumer); err != nil {
		return "", fmt.Errorf("Failed to update status for StorageConsumer %v: %v", consumer.Name, err)
	}
	consumer.Spec.Enable = true
	// update here acts as a synchronization point even if two api calls
	// resolves to a single storageconsumer the resourceVersion of one of
	// the calls will not match and be dropped
	if err := c.client.Update(ctx, consumer); err != nil {
		return "", fmt.Errorf("failed to update storageConsumer resource %q. %v", consumer.Name, err)
	}

	return string(consumer.UID), nil
}

// GetByName returns a storageConsumer resource using the Name
func (c *ocsConsumerManager) GetByName(ctx context.Context, name string) (*ocsv1alpha1.StorageConsumer, error) {

	consumerObj := &ocsv1alpha1.StorageConsumer{}
	if err := c.client.Get(ctx, types.NamespacedName{Name: name, Namespace: c.namespace}, consumerObj); err != nil {
		return nil, err
	}

	return consumerObj, nil
}

// Get returns a storageConsumer resource using the UID
func (c *ocsConsumerManager) Get(ctx context.Context, id string) (*ocsv1alpha1.StorageConsumer, error) {

	storageConsumers := &ocsv1alpha1.StorageConsumerList{}
	if err := c.client.List(
		ctx,
		storageConsumers,
		client.MatchingFields{util.ObjectUidIndexName: id},
		client.InNamespace(c.namespace),
		client.Limit(1),
	); err != nil {
		return nil, err
	}

	if len(storageConsumers.Items) == 0 {
		return nil, fmt.Errorf("no storageConsumer found with the UID %q", id)
	}

	return &storageConsumers.Items[0], nil
}

func (c *ocsConsumerManager) UpdateConsumerStatus(ctx context.Context, id string, status ifaces.StorageClientStatus) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}
	fillStorageClientInfo(&consumerObj.Status, status)
	consumerObj.Status.Client.StorageQuotaUtilizationRatio = status.GetStorageQuotaUtilizationRatio()

	if err := c.client.Status().Update(ctx, consumerObj); err != nil {
		return fmt.Errorf("Failed to patch Status for StorageConsumer %v: %v", consumerObj.Name, err)
	}
	return nil
}

func (c *ocsConsumerManager) AddAnnotation(ctx context.Context, id string, annotation, value string) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	if util.AddAnnotation(consumerObj, annotation, value) {
		if err = c.client.Update(ctx, consumerObj); err != nil {
			return fmt.Errorf(
				"failed to add annotation %s to StorageConsumer %v: %v",
				annotation,
				consumerObj.Name,
				err,
			)
		}
	}
	return nil
}

func (c *ocsConsumerManager) RemoveAnnotation(ctx context.Context, id string, annotation string) error {
	consumerObj, err := c.Get(ctx, id)
	if err != nil {
		return err
	}

	_, hasAnnotation := consumerObj.GetAnnotations()[annotation]
	if hasAnnotation {
		delete(consumerObj.GetAnnotations(), annotation)
		if err = c.client.Update(ctx, consumerObj); err != nil {
			return fmt.Errorf(
				"failed to remove annotation %s from StorageConsumer %v: %v",
				annotation,
				consumerObj.Name,
				err,
			)
		}
	}
	return nil
}

func (c *ocsConsumerManager) GetByClientID(ctx context.Context, clientID string) (*ocsv1alpha1.StorageConsumer, error) {
	consumerObjList := &ocsv1alpha1.StorageConsumerList{}
	err := c.client.List(ctx, consumerObjList, client.InNamespace(c.namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list storageConsumer objects: %v", err)
	}
	for i := range consumerObjList.Items {
		consumer := consumerObjList.Items[i]
		if consumer.Status.Client != nil && consumer.Status.Client.ID == clientID {
			return &consumer, nil
		}
	}
	return nil, nil
}

func (c *ocsConsumerManager) ClearClientInformation(ctx context.Context, consumerID string) error {
	consumer, err := c.Get(ctx, consumerID)
	if err != nil {
		return err
	}
	patch := client.RawPatch(types.MergePatchType, []byte(`{"status":{"client":null}}`))
	if err = c.client.Status().Patch(ctx, consumer, patch); err != nil {
		return fmt.Errorf("failed to remove client information from storageConsumer status %v: %v", consumer.Name, err)
	}

	return nil
}

func fillStorageClientInfo(consumerStatus *ocsv1alpha1.StorageConsumerStatus, clientInfo ifaces.StorageClientInfo) {
	if consumerStatus.Client == nil {
		consumerStatus.Client = &ocsv1alpha1.ClientStatus{}
	}
	consumerStatus.LastHeartbeat = metav1.Now()
	consumerStatus.Client.PlatformVersion = clientInfo.GetClientPlatformVersion()
	consumerStatus.Client.OperatorVersion = clientInfo.GetClientOperatorVersion()
	consumerStatus.Client.OperatorNamespace = clientInfo.GetClientOperatorNamespace()
	consumerStatus.Client.ID = clientInfo.GetClientID()
	consumerStatus.Client.ClusterName = clientInfo.GetClusterName()
	consumerStatus.Client.ClusterID = clientInfo.GetClusterID()
	consumerStatus.Client.Name = clientInfo.GetClientName()
}
