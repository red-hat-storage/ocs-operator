package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	ocsProviderServerName = "ocs-provider-server"
)

type ocsProviderServer struct{}

func (o *ocsProviderServer) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	/*
		We are moving to deploy OCSProviderServer in all modes and renaming it to OCSServer.
		The deployment is handled from CSV, whereas the service is deployed from ocsinit reconciler.
		We need to handle deletion of old secret, deployment and service so that it doesn't interfere with deployment
		of new resources.
	*/

	// handle deletion of the secret, deployment and the service
	secret := &corev1.Secret{}
	secret.Name = ocsProviderServerName
	secret.Namespace = instance.Namespace

	deployment := &appsv1.Deployment{}
	deployment.Name = ocsProviderServerName
	deployment.Namespace = instance.Namespace

	service := &corev1.Service{}
	service.Name = ocsProviderServerName
	service.Namespace = instance.Namespace

	err := r.Client.Delete(r.ctx, secret)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	err = r.Client.Delete(r.ctx, deployment)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	err = r.Client.Delete(r.ctx, service)
	if client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil

}

func (o *ocsProviderServer) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
