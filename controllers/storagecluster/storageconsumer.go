package storagecluster

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	localStorageConsumerName          = "internal"
	localStorageConsumerConfigMapName = "storageconsumer-internal"
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = localStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageConsumer, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, storageConsumer, r.Scheme); err != nil {
			return err
		}
		spec := &storageConsumer.Spec
		// will be filled by the consumer controller based on defaults
		spec.ResourceNameMappingConfigMap.Name = localStorageConsumerConfigMapName
		spec.StorageClasses = []ocsv1a1.StorageClassSpec{
			// TODO: after finding virt availability need to send corresponding sc
			{Name: generateNameForCephBlockPoolSC(storageCluster)},
			{Name: generateNameForCephFilesystemSC(storageCluster)},
		}
		spec.VolumeSnapshotClasses = []ocsv1a1.VolumeSnapshotClassSpec{
			{Name: generateNameForSnapshotClass(storageCluster, rbdSnapshotter)},
			{Name: generateNameForSnapshotClass(storageCluster, cephfsSnapshotter)},
		}
		spec.VolumeGroupSnapshotClasses = []ocsv1a1.VolumeGroupSnapshotClassSpec{
			{Name: generateNameForGroupSnapshotClass(storageCluster, rbdGroupSnapshotter)},
			{Name: generateNameForGroupSnapshotClass(storageCluster, cephfsGroupSnapshotter)},
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update storageconsumer %s: %v", storageConsumer.Name, err)
	}
	return ctrl.Result{}, nil
}

func (s *storageConsumer) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (ctrl.Result, error) {
	// cleaned up via owner references
	return ctrl.Result{}, nil
}
