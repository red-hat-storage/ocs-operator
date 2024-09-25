package storagecluster

import (
	"fmt"

	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
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

	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storagecluster.Name
	_, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageClient, func() error {
		if storageClient.Status.ConsumerID == "" {
			token, err := util.GenerateOnboardingToken(tokenLifetimeInHours, onboardingPrivateKeyFilePath)
			if err != nil {
				return fmt.Errorf("unable to generate onboarding token: %v", err)
			}
			storageClient.Spec.OnboardingTicket = token
		}
		// we could just use svcName:port however in-cluster traffic from "*.svc" is generally not proxied and
		// we using qualified name upto ".svc" makes connection not go through any proxies.
		storageClient.Spec.StorageProviderEndpoint = fmt.Sprintf("%s.%s.svc:%d", ocsProviderServerName, storagecluster.Namespace, ocsProviderServicePort)
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
