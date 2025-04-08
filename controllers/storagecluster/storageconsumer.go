package storagecluster

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	localStorageConsumerConfigMapName = "storageconsumer-internal"
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	// TODO (leelavg): revisit in 4.20 whether this can be retired or not
	// If the cluster was configured in provider mode pre4.19 a storageconsumer would already exist for serving storage to local services.
	// We use allowRemoteStorageConsumers for finding the older provider mode as this field is made immutable from 4.19.
	if storageCluster.Spec.AllowRemoteStorageConsumers {
		storageConsumerList := &ocsv1a1.StorageConsumerList{}
		if err := r.List(r.ctx, storageConsumerList, client.InNamespace(r.OperatorNamespace), client.Limit(1)); err != nil {
			return ctrl.Result{}, fmt.Errorf("failed to list storageconsumers: %v", err)
		}
		if len(storageConsumerList.Items) > 0 {
			r.Log.Info("Skipping creation of internal storageconsumer as the cluster was configured in provider mode pre4.19")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("expected storageconsumer to exist from pre4.19 provider mode")
	}

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
			{Name: util.GenerateNameForCephBlockPoolStorageClass(storageCluster)},
			{Name: util.GenerateNameForCephFilesystemStorageClass(storageCluster)},
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
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster)},
			)
		}

		if storageCluster.Spec.ManagedResources.CephNonResilientPools.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForNonResilientCephBlockPoolStorageClass(storageCluster)},
			)
		}

		if storageCluster.Spec.NFS != nil && storageCluster.Spec.NFS.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForCephNetworkFilesystemStorageClass(storageCluster)},
			)
			spec.VolumeSnapshotClasses = append(
				spec.VolumeSnapshotClasses,
				ocsv1a1.VolumeSnapshotClassSpec{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.NfsSnapshotter)},
			)
		}
		if storageCluster.Spec.Encryption.StorageClass && storageCluster.Spec.Encryption.KeyManagementService.Enable {
			spec.StorageClasses = append(
				spec.StorageClasses,
				ocsv1a1.StorageClassSpec{Name: util.GenerateNameForEncryptedCephBlockPoolStorageClass(storageCluster)},
			)
		}

		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update storageconsumer %s: %v", storageConsumer.Name, err)
	}
	if storageConsumer.UID == "" {
		return ctrl.Result{}, fmt.Errorf("expected storageConsumer UID to not be empty")
	}

	availableServices, err := util.GetAvailableServices(r.ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get available services configured in StorageCluster: %v", err)
	}
	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Name = localStorageConsumerConfigMapName
	consumerConfigMap.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, consumerConfigMap, func() error {
		data := util.GetStorageConsumerDefaultResourceNames(
			defaults.LocalStorageConsumerName,
			string(storageConsumer.UID),
			availableServices,
		)
		resourceMap := util.WrapStorageConsumerResourceMap(data)
		resourceMap.ReplaceRbdRadosNamespaceName(util.ImplicitRbdRadosNamespaceName)
		resourceMap.ReplaceSubVolumeGroupName("csi")
		resourceMap.ReplaceSubVolumeGroupRadosNamespaceName("csi")
		resourceMap.ReplaceRbdClientProfileName("openshift-storage")
		resourceMap.ReplaceCephFsClientProfileName("openshift-storage")
		resourceMap.ReplaceNfsClientProfileName("openshift-storage")
		// NB: Do we need to allow user changing/overwriting any values in this configmap?
		consumerConfigMap.Data = data
		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update storageconsumer configmap %s: %v", localStorageConsumerConfigMapName, err)
	}

	return ctrl.Result{}, nil
}

func (s *storageConsumer) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (ctrl.Result, error) {
	// cleaned up via owner references
	return ctrl.Result{}, nil
}
