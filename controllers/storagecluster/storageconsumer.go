package storagecluster

import (
	"fmt"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	localStorageConsumerConfigMapName = "storageconsumer-internal"
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = defaults.LocalStorageConsumerName
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
			{Name: util.GenerateNameForCephBlockPoolSC(storageCluster)},
			{Name: util.GenerateNameForCephFilesystemSC(storageCluster)},
		}
		spec.VolumeSnapshotClasses = []ocsv1a1.VolumeSnapshotClassSpec{
			{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.RbdSnapshotter)},
			{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter)},
		}
		spec.VolumeGroupSnapshotClasses = []ocsv1a1.VolumeGroupSnapshotClassSpec{
			{Name: util.GenerateNameForGroupSnapshotClass(storageCluster, util.RbdGroupSnapshotter)},
			{Name: util.GenerateNameForGroupSnapshotClass(storageCluster, util.CephfsGroupSnapshotter)},
		}

		crd := &metav1.PartialObjectMetadata{}
		crd.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
		crd.Name = VirtualMachineCrdName
		if err := r.Client.Get(r.ctx, client.ObjectKeyFromObject(crd), crd); client.IgnoreNotFound(err) != nil {
			return err
		}

		if crd.UID != "" {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForCephBlockPoolVirtualizationSC(storageCluster)},
			)
		}

		if storageCluster.Spec.ManagedResources.CephNonResilientPools.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForNonResilientCephBlockPoolSC(storageCluster)},
			)
		}

		if storageCluster.Spec.NFS != nil && storageCluster.Spec.NFS.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForCephNetworkFilesystemSC(storageCluster)},
			)
			spec.VolumeSnapshotClasses = append(
				spec.VolumeSnapshotClasses,
				ocsv1a1.VolumeSnapshotClassSpec{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.NfsSnapshotter)},
			)
		}
		if storageCluster.Spec.Encryption.StorageClass && storageCluster.Spec.Encryption.KeyManagementService.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForEncryptedCephBlockPoolSC(storageCluster)},
			)
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
