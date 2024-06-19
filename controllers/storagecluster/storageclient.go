package storagecluster

import (
	"fmt"

	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"golang.org/x/exp/maps"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	tokenLifetimeInHours         = 48
	onboardingPrivateKeyFilePath = "/etc/private-key/key"
)

type storageClient struct{}

var _ resourceManager = &storageClient{}

func (s *storageClient) ensureCreated(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {

	if !storagecluster.Spec.AllowRemoteStorageConsumers {
		r.Log.Info("Spec.AllowRemoteStorageConsumers is disabled")
		return s.ensureDeleted(r, storagecluster)
	}

	if err := s.updateClientConfigMap(r, storagecluster.Namespace); err != nil {
		return reconcile.Result{}, err
	}

	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageClient, func() error {
		if storageClient.Status.ConsumerID == "" {
			token, err := util.GenerateOnboardingToken(tokenLifetimeInHours, onboardingPrivateKeyFilePath, nil)
			if err != nil {
				return fmt.Errorf("unable to generate onboarding token: %v", err)
			}
			storageClient.Spec.OnboardingTicket = token
		}
		// the controller of storageclient is running in same namespace and should be able to resolve the endpoint
		// via servicename:serviceport irrespective of clusterip/nodeport/lb
		storageClient.Spec.StorageProviderEndpoint = fmt.Sprintf("%s:%d", ocsProviderServerName, ocsProviderServicePort)
		return nil
	})
	if err != nil {
		r.Log.Error(err, "Failed to create local StorageClient CR")
		return reconcile.Result{}, nil
	}

	return reconcile.Result{}, nil
}

func (s *storageClient) ensureDeleted(r *StorageClusterReconciler, storagecluster *ocsv1.StorageCluster) (reconcile.Result, error) {
	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	if err := r.Delete(r.ctx, storageClient); err != nil && !kerrors.IsNotFound(err) {
		r.Log.Error(err, "Failed to initiate deletion of local StorageClient CR")
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func (s *storageClient) updateClientConfigMap(r *StorageClusterReconciler, namespace string) error {
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
	clientConfig.Data[deployCSIKey] = "true"

	if !maps.Equal(clientConfig.Data, existingData) {
		if err := r.Client.Update(r.ctx, clientConfig); err != nil {
			r.Log.Error(err, "failed to update ocs client configmap for enabling CSI")
			return err
		}
	}

	return nil
}
