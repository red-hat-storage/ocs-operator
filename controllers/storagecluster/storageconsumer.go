package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

const localStorageConsumerName = "storageconsumer-local"

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = localStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageConsumer, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, storageConsumer, r.Scheme); err != nil {
			return err
		}

		// TODO: OldModeAnnotation should be removed after convergence
		util.AddAnnotation(storageConsumer, defaults.StorageConsumerOldModeAnnotation, defaults.StorageConsumerOldModeInternal)
		util.AddAnnotation(storageConsumer, defaults.StorageConsumerTypeAnnotation, defaults.StorageConsumerTypeLocal)
		return nil
	}); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{}, nil
}

func (s *storageConsumer) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (ctrl.Result, error) {
	// cleaned up via owner references
	return ctrl.Result{}, nil
}
