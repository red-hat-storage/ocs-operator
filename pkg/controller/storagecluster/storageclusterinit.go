package storagecluster

import (
	"context"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ensurestorageclusterinit function initialize the StorageCluster
func (r *ReconcileStorageCluster) ensurestorageclusterinit(
	instance *ocsv1.StorageCluster,
	request reconcile.Request,
	reqLogger logr.Logger) error {
	// Check for StorageClusterInitialization
	scinit := &ocsv1.StorageClusterInitialization{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, scinit); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating StorageClusterInitialization resource")

			// if the StorageClusterInitialization object doesn't exist
			// ensure we re-reconcile on all initialization resources
			instance.Status.StorageClassesCreated = false
			instance.Status.CephBlockPoolsCreated = false
			instance.Status.CephFilesystemsCreated = false
			instance.Status.FailureDomain = determineFailureDomain(instance)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				return err
			}

			scinit.Name = request.Name
			scinit.Namespace = request.Namespace
			if err = controllerutil.SetControllerReference(instance, scinit, r.scheme); err != nil {
				return err
			}

			err = r.client.Create(context.TODO(), scinit)
			switch {
			case err == nil:
				log.Info("Created StorageClusterInitialization resource")
			case errors.IsAlreadyExists(err):
				log.Info("StorageClusterInitialization resource already exists")
			default:
				log.Error(err, "Failed to create StorageClusterInitialization resource")
				return err
			}
		} else {
			// Error reading the object - requeue the request.
			return err
		}
	}

	return nil
}
