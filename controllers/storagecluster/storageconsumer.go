package storagecluster

import (
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	localStorageConsumerName          = defaultSubvolumeGroupName
	localStorageConsumerConfigMapName = localStorageConsumerName + "-consumer-resources"
	// storageconsumer owned resource names
	radosNamespaceNameKey             = "rados-namespace-name"
	subVolumeGroupNameKey             = "svg-name"
	csiRbdProvisionerSecretNameKey    = "csi-rbd-provisioner-secret-name"
	csiRbdNodeSecretNameKey           = "csi-rbd-node-secret-name"
	csiCephFsProvisionerSecretNameKey = "csi-cephfs-provisioner-secret-name"
	csiCephFsNodeSecretNameKey        = "csi-cephfs-node-secret-name"
	rbdClientProfileNameKey           = "rbd-client-profile"
	cephFsClientProfileNameKey        = "cephfs-client-profile"
	nfsClientProfileNameKey           = "nfs-client-profile"
	svgMetadataRadosNamespaceNameKey  = "svg-metadata-rns-name"
)

type storageConsumer struct{}

var _ resourceManager = &storageConsumer{}

func (s *storageConsumer) ensureCreated(r *StorageClusterReconciler, storageCluster *ocsv1.StorageCluster) (ctrl.Result, error) {
	// get ceph fsid to be used in clientprofile naming
	cephCluster := &rookv1.CephCluster{}
	cephCluster.Name = generateNameForCephCluster(storageCluster)
	cephCluster.Namespace = storageCluster.Namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(cephCluster), cephCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get ceph cluster %s: %v", cephCluster.Name, err)
	}
	cephClusterFsid := cephCluster.Status.CephStatus.FSID
	if cephClusterFsid == "" {
		return ctrl.Result{}, fmt.Errorf("cephcluster fsid is empty")
	}

	// in provider mode, storageconsumer has ocp clusterid as it's suffix and
	// if a consumer matching the suffix exists we should not create local storageconsumer
	clusterID := util.GetClusterID(r.ctx, r.Client, &r.Log)
	if clusterID == "" {
		return ctrl.Result{}, fmt.Errorf("failed to get OCP cluster id")
	}
	storageConsumer := &ocsv1a1.StorageConsumer{}
	storageConsumer.Name = fmt.Sprintf("storageconsumer-%s", clusterID)
	storageConsumer.Namespace = storageCluster.Namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get storageconsumer %s", storageConsumer.Name)
	} else if storageConsumer.UID != "" {
		r.Log.Info("not creating local storageconsumer due to existence of storageconsumer resembling provider mode", "StorageConsumer", storageConsumer.Name)
		return ctrl.Result{}, nil
	}

	// check if compatibility annotation is already set on consumer
	compatibleMode := ""
	storageConsumer.Name = localStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if err := r.Get(r.ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); client.IgnoreNotFound(err) != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get storageconsumer %s", storageConsumer.Name)
	} else if storageConsumer.UID != "" {
		// we create consumer with annotation and is resistant against api server -> manager cache latency
		compatibleMode = storageConsumer.GetAnnotations()[defaults.StorageConsumerBackwardCompatibleAnnotation]
	}

	// decide between upgraded internal or fresh install
	if compatibleMode == "" {
		compatibleMode = defaults.StorageConsumerBackwardCompatibleModeNone
		svg := &rookv1.CephFilesystemSubVolumeGroup{}
		svg.Name = generateNameForCephSubvolumeGroup(cephCluster.Name)
		svg.Namespace = storageCluster.Namespace
		if err := r.Get(r.ctx, client.ObjectKeyFromObject(svg), svg); client.IgnoreNotFound(err) != nil {
			return ctrl.Result{}, fmt.Errorf("failed to get subvolumegroup %s", svg.Name)
		} else if svg.UID != "" {
			if storageClusterIsOwner, err := controllerutil.HasOwnerReference(svg.OwnerReferences, storageCluster, r.Scheme); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to find owner reference on subvolumegroup %s", svg.Name)
			} else if storageClusterIsOwner {
				// consumer controller will not reconcile this consumer until created and we find the mode as
				// 1. in an upgraded cluster svg owner is still storagecluster till consumer controller changes the ownership
				// 2. we will place the annotation on storageconsumer and will remove this code from next release (4.20)
				compatibleMode = defaults.StorageConsumerBackwardCompatibleModeInternal
			}
		}
	}

	// even in DR scenarios local consumer resources aren't shared and resources in this configmap is exclusively owned by this consumer
	configMap := &corev1.ConfigMap{}
	configMap.Name = localStorageConsumerConfigMapName
	configMap.Namespace = storageCluster.Namespace
	clientProfileName := util.CalculateMD5Hash([2]string{cephClusterFsid, localStorageConsumerName})
	if len(clientProfileName) > 36 {
		// truncating at 36 as that is the maximum limit that csi can use in it's handle
		clientProfileName = clientProfileName[:36]
	}
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, configMap, func() error {
		if configMap.Data == nil {
			configMap.Data = map[string]string{}
		}
		resources := configMap.Data
		if compatibleMode == defaults.StorageConsumerBackwardCompatibleModeInternal {
			resources[radosNamespaceNameKey] = ""
			resources[subVolumeGroupNameKey] = "csi"
			resources[csiRbdProvisionerSecretNameKey] = "rook-csi-rbd-provisioner"
			resources[csiRbdNodeSecretNameKey] = "rook-csi-rbd-node"
			resources[csiCephFsProvisionerSecretNameKey] = "rook-csi-cephfs-provisioner"
			resources[csiCephFsNodeSecretNameKey] = "rook-csi-cephfs-node"
			resources[rbdClientProfileNameKey] = "openshift-storage"
			resources[cephFsClientProfileNameKey] = "openshift-storage"
			resources[nfsClientProfileNameKey] = "openshift-storage"
			resources[svgMetadataRadosNamespaceNameKey] = "csi"
		} else {
			resources[radosNamespaceNameKey] = localStorageConsumerName
			resources[subVolumeGroupNameKey] = localStorageConsumerName
			resources[csiRbdProvisionerSecretNameKey] = fmt.Sprintf("rbd-provisioner-%s", clientProfileName)
			resources[csiRbdNodeSecretNameKey] = fmt.Sprintf("rbd-node-%s", clientProfileName)
			resources[csiCephFsProvisionerSecretNameKey] = fmt.Sprintf("cephfs-provisioner-%s", clientProfileName)
			resources[csiCephFsNodeSecretNameKey] = fmt.Sprintf("cephfs-node-%s", clientProfileName)
			resources[rbdClientProfileNameKey] = clientProfileName
			resources[cephFsClientProfileNameKey] = clientProfileName
			resources[nfsClientProfileNameKey] = clientProfileName
			resources[svgMetadataRadosNamespaceNameKey] = localStorageConsumerName
		}
		return nil
	}); err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to create/update configmap %s: %v", configMap.Name, err)
	}

	storageConsumer.Name = localStorageConsumerName
	storageConsumer.Namespace = storageCluster.Namespace
	if _, err := controllerutil.CreateOrUpdate(r.ctx, r.Client, storageConsumer, func() error {
		if err := controllerutil.SetControllerReference(storageCluster, storageConsumer, r.Scheme); err != nil {
			return err
		}
		// TODO: BackwardCompatibleAnnotation could be removed after convergence
		util.AddAnnotation(storageConsumer, defaults.StorageConsumerBackwardCompatibleAnnotation, compatibleMode)
		util.AddAnnotation(storageConsumer, defaults.StorageConsumerTypeAnnotation, defaults.StorageConsumerTypeLocal)

		spec := &storageConsumer.Spec
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
