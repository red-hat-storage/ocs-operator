package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	configv1 "github.com/openshift/api/config/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	statusutil "github.com/red-hat-storage/ocs-operator/controllers/util"
	externalClient "github.com/red-hat-storage/ocs-operator/services/provider/client"
)

var (
	// externalOCSResources will hold the ExternalResources for storageclusters
	// ExternalResources can be accessible using the UID of an storagecluster
	externalOCSResources = map[types.UID][]ExternalResource{}
)

const (
	// grpcCallNames
	OnboardConsumer       = "OnboardConsumer"
	OffboardConsumer      = "OffboardConsumer"
	UpdateCapacity        = "UpdateCapacity"
	GetStorageConfig      = "GetStorageConfig"
	AcknowledgeOnboarding = "AcknowledgeOnboarding"
)

// IsOCSConsumerMode returns true if it is ocs to ocs ExternalStorage consumer cluster
func IsOCSConsumerMode(instance *ocsv1.StorageCluster) bool {
	return instance.Spec.ExternalStorage.Enable && instance.Spec.ExternalStorage.StorageProviderKind == ocsv1.KindOCS
}

// newExternalClusterClient returns the *externalClient.OCSProviderClient
func (r *StorageClusterReconciler) newExternalClusterClient(instance *ocsv1.StorageCluster) (*externalClient.OCSProviderClient, error) {

	consumerClient, err := externalClient.NewProviderClient(
		context.Background(), instance.Spec.ExternalStorage.StorageProviderEndpoint, time.Second*10)
	if err != nil {
		return nil, err
	}

	return consumerClient, nil
}

// onboardConsumer makes an API call to the external storage provider cluster for onboarding
func (r *StorageClusterReconciler) onboardConsumer(instance *ocsv1.StorageCluster, externalClusterClient *externalClient.OCSProviderClient) (reconcile.Result, error) {

	clusterVersion := &configv1.ClusterVersion{}
	err := r.Client.Get(context.Background(), types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		r.Log.Error(err, "External-OCS:Failed to get the clusterVersion version of the OCP cluster")
		return reconcile.Result{}, err
	}

	name := fmt.Sprintf("storageconsumer-%s", clusterVersion.Spec.ClusterID)
	response, err := externalClusterClient.OnboardConsumer(
		context.Background(), instance.Spec.ExternalStorage.OnboardingTicket, name,
		instance.Spec.ExternalStorage.RequestedCapacity.String())
	if err != nil {
		if s, ok := status.FromError(err); ok {
			r.logGrpcErrorAndReportEvent(instance, OnboardConsumer, err, s.Code())
		}
		return reconcile.Result{}, err
	}

	if response.StorageConsumerUUID == "" || response.GrantedCapacity == "" {
		err = fmt.Errorf("External-OCS:OnboardConsumer:response is empty")
		r.Log.Error(err, "empty response")
		return reconcile.Result{}, err
	}

	instance.Status.ExternalStorage.ConsumerID = response.StorageConsumerUUID
	instance.Status.ExternalStorage.GrantedCapacity = resource.MustParse(response.GrantedCapacity)
	instance.Status.Phase = statusutil.PhaseOnboarding

	r.Log.Info("External-OCS:Onboarding is succeed, will save status.")
	return reconcile.Result{Requeue: true}, nil
}

func (r *StorageClusterReconciler) acknowledgeOnboarding(instance *ocsv1.StorageCluster, externalClusterClient *externalClient.OCSProviderClient) (reconcile.Result, error) {

	_, err := externalClusterClient.AcknowledgeOnboarding(context.Background(), instance.Status.ExternalStorage.ConsumerID)
	if err != nil {
		if s, ok := status.FromError(err); ok {
			r.logGrpcErrorAndReportEvent(instance, AcknowledgeOnboarding, err, s.Code())
		}
		r.Log.Error(err, "External-OCS:Failed to acknowledge onboarding.")
		return reconcile.Result{}, err
	}

	instance.Status.Phase = statusutil.PhaseProgressing

	r.Log.Info("External-OCS:Onboarding is acknowledged successfully.")
	return reconcile.Result{Requeue: true}, nil
}

// offboardConsumer makes an API call to the external storage provider cluster for offboarding
func (r *StorageClusterReconciler) offboardConsumer(instance *ocsv1.StorageCluster, externalClusterClient *externalClient.OCSProviderClient) (reconcile.Result, error) {

	_, err := externalClusterClient.OffboardConsumer(context.Background(), instance.Status.ExternalStorage.ConsumerID)
	if err != nil {
		if s, ok := status.FromError(err); ok {
			r.logGrpcErrorAndReportEvent(instance, OffboardConsumer, err, s.Code())
		}
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// updateConsumerCapacity makes an API call to the external storage provider cluster to update the capacity
func (r *StorageClusterReconciler) updateConsumerCapacity(instance *ocsv1.StorageCluster, externalClusterClient *externalClient.OCSProviderClient) (reconcile.Result, error) {

	response, err := externalClusterClient.UpdateCapacity(
		context.Background(),
		instance.Status.ExternalStorage.ConsumerID,
		instance.Spec.ExternalStorage.RequestedCapacity.String())
	if err != nil {
		if s, ok := status.FromError(err); ok {
			r.logGrpcErrorAndReportEvent(instance, UpdateCapacity, err, s.Code())
		}
		return reconcile.Result{}, err
	}

	responseQuantity, err := resource.ParseQuantity(response.GrantedCapacity)
	if err != nil {
		r.Log.Error(err, "Failed to parse GrantedCapacity from UpdateCapacity response.", "GrantedCapacity", response.GrantedCapacity)
		return reconcile.Result{}, err
	}

	if !instance.Spec.ExternalStorage.RequestedCapacity.Equal(responseQuantity) {
		klog.Warningf("GrantedCapacity is not equal to the RequestedCapacity in the UpdateCapacity response.",
			"GrantedCapacity", response.GrantedCapacity, "RequestedCapacity", instance.Spec.ExternalStorage.RequestedCapacity)
	}

	instance.Status.ExternalStorage.GrantedCapacity = responseQuantity

	return reconcile.Result{}, nil
}

// getExternalConfigFromProvider makes an API call to the external storage provider cluster for json blob
func (r *StorageClusterReconciler) getExternalConfigFromProvider(
	instance *ocsv1.StorageCluster, externalClusterClient *externalClient.OCSProviderClient) ([]ExternalResource, reconcile.Result, error) {

	response, err := externalClusterClient.GetStorageConfig(context.Background(), instance.Status.ExternalStorage.ConsumerID)
	if err != nil {
		if s, ok := status.FromError(err); ok {
			r.logGrpcErrorAndReportEvent(instance, GetStorageConfig, err, s.Code())

			// storage consumer is not ready yet, requeue after some time
			if s.Code() == codes.Unavailable {
				return nil, reconcile.Result{RequeueAfter: time.Second * 5}, nil
			}
		}

		return nil, reconcile.Result{}, err
	}

	var externalResources []ExternalResource

	for _, eResource := range response.ExternalResource {

		data := map[string]string{}
		err = json.Unmarshal(eResource.Data, &data)
		if err != nil {
			r.Log.Error(err, "Failed to Unmarshal response of GetStorageConfig", "Kind", eResource.Kind, "Name", eResource.Name, "Data", eResource.Data)
			return nil, reconcile.Result{}, err
		}

		externalResources = append(externalResources, ExternalResource{
			Kind: eResource.Kind,
			Data: data,
			Name: eResource.Name,
		})
	}

	return externalResources, reconcile.Result{}, nil
}

func (r *StorageClusterReconciler) logGrpcErrorAndReportEvent(instance *ocsv1.StorageCluster, grpcCallName string, err error, errCode codes.Code) {

	var msg, eventReason, eventType string

	if grpcCallName == OnboardConsumer {
		if errCode == codes.InvalidArgument {
			msg = "Token is invalid. Verify the token again or contact the provider admin"
			eventReason = "TokenInvalid"
			eventType = corev1.EventTypeWarning
		} else if errCode == codes.AlreadyExists {
			msg = "Token is already used. Contact provider admin for a new token"
			eventReason = "TokenAlreadyUsed"
			eventType = corev1.EventTypeWarning
		}
	} else if grpcCallName == AcknowledgeOnboarding {
		if errCode == codes.NotFound {
			msg = "StorageConsumer not found. Contact the provider admin"
			eventReason = "NotFound"
			eventType = corev1.EventTypeWarning
		}
	} else if grpcCallName == OffboardConsumer {
		if errCode == codes.InvalidArgument {
			msg = "StorageConsumer UID is not valid. Contact the provider admin"
			eventReason = "UIDInvalid"
			eventType = corev1.EventTypeWarning
		}
	} else if grpcCallName == UpdateCapacity {
		if errCode == codes.InvalidArgument {
			msg = "StorageConsumer UID or requested capacity is not valid. Contact the provider admin"
			eventReason = "UIDorCapacityInvalid"
			eventType = corev1.EventTypeWarning
		} else if errCode == codes.NotFound {
			msg = "StorageConsumer UID not found. Contact the provider admin"
			eventReason = "UIDNotFound"
			eventType = corev1.EventTypeWarning
		}
	} else if grpcCallName == GetStorageConfig {
		if errCode == codes.InvalidArgument {
			msg = "StorageConsumer UID is not valid. Contact the provider admin"
			eventReason = "UIDInvalid"
			eventType = corev1.EventTypeWarning
		} else if errCode == codes.NotFound {
			msg = "StorageConsumer UID not found. Contact the provider admin"
			eventReason = "UIDNotFound"
			eventType = corev1.EventTypeWarning
		} else if errCode == codes.Unavailable {
			msg = "StorageConsumer is not ready yet. Will requeue after 5 second"
			eventReason = "NotReady"
			eventType = corev1.EventTypeNormal
		}
	}

	if msg != "" {
		r.Log.Error(err, "External-OCS:"+grpcCallName+":"+msg)
		r.recorder.ReportIfNotPresent(instance, eventType, eventReason, msg)
	}
}
