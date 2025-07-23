package storagecluster

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"

	nadv1 "github.com/k8snetworkplumbingwg/network-attachment-definition-client/pkg/apis/k8s.cni.cncf.io/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ocsClientConfigMapName             = "ocs-client-operator-config"
	manageNoobaaSubKey                 = "manageNoobaaSubscription"
	useHostNetworkForCsiControllersKey = "useHostNetworkForCsiControllers"
	disableVersionChecksKey            = "disableVersionChecks"
	// cephNetworkAnnotationKey is the annotation key used to store network details used by ceph
	cniNetworksAnnotationKey = "k8s.v1.cni.cncf.io/networks"
)

type storageClient struct{}

var _ resourceManager = &storageClient{}

func (s *storageClient) ensureCreated(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if !r.AvailableCrds[StorageClientCrdName] {
		return reconcile.Result{}, fmt.Errorf("StorageClient CRD is not available")
	}

	if err := s.updateClientConfigMap(r, storagecluster.Namespace, storagecluster.Spec.HostNetwork); err != nil {
		return reconcile.Result{}, err
	}

	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageClient, func() error {
		if storageClient.Status.ConsumerID == "" {
			localStorageConsumer := &ocsv1a1.StorageConsumer{}
			localStorageConsumer.Name = defaults.LocalStorageConsumerName
			localStorageConsumer.Namespace = storagecluster.Namespace
			if err := r.Get(r.ctx, client.ObjectKeyFromObject(localStorageConsumer), localStorageConsumer); err != nil {
				return fmt.Errorf("failed to get storageconsumer %s: %v", localStorageConsumer.Name, err)
			} else if localStorageConsumer.Status.OnboardingTicketSecret.Name == "" {
				return fmt.Errorf("no reference to onboarding secret found in storageconsumer %s status", localStorageConsumer.Name)
			}

			onboardingSecret := &corev1.Secret{}
			onboardingSecret.Name = localStorageConsumer.Status.OnboardingTicketSecret.Name
			onboardingSecret.Namespace = storagecluster.Namespace
			if err := r.Get(r.ctx, client.ObjectKeyFromObject(onboardingSecret), onboardingSecret); err != nil {
				return fmt.Errorf("failed to get onboarding secret %s: %v", onboardingSecret.Name, err)
			} else if len(onboardingSecret.Data[defaults.OnboardingTokenKey]) == 0 {
				return fmt.Errorf("no 'onboarding-token' field found in onboarding secret %s", onboardingSecret.Name)
			}

			storageClient.Spec.OnboardingTicket = string(onboardingSecret.Data[defaults.OnboardingTokenKey])
		}
		// we could just use svcName:port however in-cluster traffic from "*.svc" is generally not proxied and
		// we using qualified name upto ".svc" makes connection not go through any proxies.
		storageClient.Spec.StorageProviderEndpoint = fmt.Sprintf("%s.%s.svc:%d", ocsProviderServerName, storagecluster.Namespace, ocsProviderServicePort)

		if storagecluster.Spec.Network != nil && storagecluster.Spec.Network.IsMultus() {
			cephNWAnnotationValue, err := getCephNetworkAnnotationValue(storagecluster.Spec.Network, storagecluster.Namespace)
			if err != nil {
				return fmt.Errorf("failed to get Ceph network annotation value: %v", err)
			}
			util.AddAnnotation(storageClient, cniNetworksAnnotationKey, cephNWAnnotationValue)
		}

		controllerutil.AddFinalizer(storageClient, internalComponentFinalizer)

		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create local StorageClient CR")
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (s *storageClient) ensureDeleted(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	if !r.AvailableCrds[StorageClientCrdName] {
		r.Log.Info("StorageClient CRD doesn't exist and not proceeding with deletion of storageclient CR (if any)")
		return reconcile.Result{}, nil
	}
	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name

	if err := r.Delete(r.ctx, storageClient); err != nil && !kerrors.IsNotFound(err) {
		r.Log.Error(err, "Failed to initiate deletion of local StorageClient CR")
		return reconcile.Result{}, err
	}

	if controllerutil.RemoveFinalizer(storageClient, internalComponentFinalizer) {
		r.Log.Info("Removing finalizer from StorageClient.", "StorageClient:", storageClient.Name, " Finalizer:", internalComponentFinalizer)
		if err := r.Update(r.ctx, storageClient); err != nil {
			r.Log.Info("Failed to remove finalizer from StorageClient.", "StorageClient:", storageClient.Name, " Finalizer:", internalComponentFinalizer)
			return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageClient: %v", err)
		}
	}
	return reconcile.Result{}, nil
}

func (s *storageClient) updateClientConfigMap(r *StorageClusterReconciler, namespace string, useHostNetworkForCsiControllers bool) error {
	clientConfig := &corev1.ConfigMap{}
	clientConfig.Name = ocsClientConfigMapName
	clientConfig.Namespace = namespace

	if err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(clientConfig), clientConfig); err != nil {
		r.Log.Error(err, "failed to get ocs client configmap")
		return err
	}

	existingData := maps.Clone(clientConfig.Data)
	if clientConfig.Data == nil {
		clientConfig.Data = map[string]string{}
	}
	clientConfig.Data[manageNoobaaSubKey] = strconv.FormatBool(false)
	clientConfig.Data[useHostNetworkForCsiControllersKey] = strconv.FormatBool(useHostNetworkForCsiControllers)
	clientConfig.Data[disableVersionChecksKey] = strconv.FormatBool(true)

	if !maps.Equal(clientConfig.Data, existingData) {
		if err := r.Client.Update(r.ctx, clientConfig); err != nil {
			r.Log.Error(err, "failed to update client operator's configmap data")
			return err
		}
	}

	return nil
}

// getCephNetworkAnnotationValue returns the network annotation value for the given StorageCluster NetworkSpec.
func getCephNetworkAnnotationValue(cephNetworkSpec *rookCephv1.NetworkSpec, scNamespace string) (string, error) {
	if len(cephNetworkSpec.Selectors) == 0 {
		return "", fmt.Errorf("invalid ceph network spec")
	}
	networkSelectionElement, err := cephNetworkSpec.GetNetworkSelection(scNamespace, rookCephv1.CephNetworkType("public"))
	if err != nil || networkSelectionElement == nil {
		return "", fmt.Errorf("failed to get network selection element: %v", err)
	}
	nwAnnotation, err := json.Marshal([]*nadv1.NetworkSelectionElement{networkSelectionElement})
	if err != nil {
		return "", fmt.Errorf("failed to marshal network selection elements: %v", err)
	}
	return string(nwAnnotation), nil
}
