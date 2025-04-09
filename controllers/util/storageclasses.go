package util

import (
	"context"
	"errors"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"
	storageIdLabelKey             = "ramendr.openshift.io/storageid"
)

var (
	UnsupportedProvisioner = errors.New("unsupportedProvisioner")
)

func GenerateNameForCephBlockPoolStorageClass(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephBlockPools.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephBlockPools.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd", storageCluster.Name)
}

func GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephBlockPools.VirtualizationStorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephBlockPools.VirtualizationStorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd-virtualization", storageCluster.Name)
}

func GenerateNameForNonResilientCephBlockPoolStorageClass(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephNonResilientPools.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephNonResilientPools.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-non-resilient-rbd", storageCluster.Name)
}

func GenerateNameForCephFilesystemStorageClass(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephFilesystems.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephFilesystems.StorageClassName
	}
	return fmt.Sprintf("%s-cephfs", storageCluster.Name)
}

func GenerateNameForCephRgwStorageClass(initData *ocsv1.StorageCluster) string {
	if initData.Spec.ManagedResources.CephObjectStores.StorageClassName != "" {
		return initData.Spec.ManagedResources.CephObjectStores.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rgw", initData.Name)
}

func GenerateNameForEncryptedCephBlockPoolStorageClass(initData *ocsv1.StorageCluster) string {
	if initData.Spec.Encryption.StorageClassName != "" {
		return initData.Spec.Encryption.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd-encrypted", initData.Name)
}

func GenerateNameForCephNetworkFilesystemStorageClass(initData *ocsv1.StorageCluster) string {
	if initData.Spec.NFS != nil && initData.Spec.NFS.StorageClassName != "" {
		return initData.Spec.NFS.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-nfs", initData.Name)
}

func NewDefaultRbdStorageClass(
	clusterID,
	poolName,
	provisionerSecret,
	nodeSecret,
	namespace,
	storageId string,
	isDefaultStorageClass bool,
) *storagev1.StorageClass {

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				"reclaimspace.csiaddons.openshift.io/schedule": "@weekly",
			},
			Labels: map[string]string{},
		},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Provisioner:          RbdDriverName,
		Parameters: map[string]string{
			"clusterID":                 clusterID,
			"pool":                      poolName,
			"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
			"csi.storage.k8s.io/fstype": "ext4",
			"imageFormat":               "2",
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}

	if isDefaultStorageClass {
		AddAnnotation(sc, defaultStorageClassAnnotation, "true")
	}
	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultVirtRbdStorageClass(
	clusterID,
	poolName,
	provisionerSecret,
	nodeSecret,
	namespace,
	storageId string,
) *storagev1.StorageClass {

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Block volumes suitable for Virtual Machine disks",
				"reclaimspace.csiaddons.openshift.io/schedule":   "@weekly",
				"storageclass.kubevirt.io/is-default-virt-class": "true",
			},
			Labels: map[string]string{},
		},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Provisioner:          RbdDriverName,
		Parameters: map[string]string{
			"clusterID":                 clusterID,
			"pool":                      poolName,
			"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
			"csi.storage.k8s.io/fstype": "ext4",
			"imageFormat":               "2",
			"mounter":                   "rbd",
			"mapOptions":                "krbd:rxbounce",
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}

	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultEncryptedRbdStorageClass(
	clusterID,
	poolName,
	provisionerSecret,
	nodeSecret,
	namespace,
	encryptionServiceName string,
	disableKeyRotation bool,
) *storagev1.StorageClass {

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				"reclaimspace.csiaddons.openshift.io/schedule": "@weekly",
				"cdi.kubevirt.io/clone-strategy":               "copy",
			},
			Labels: map[string]string{},
		},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Provisioner:          RbdDriverName,
		Parameters: map[string]string{
			"clusterID":                 clusterID,
			"pool":                      poolName,
			"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
			"csi.storage.k8s.io/fstype": "ext4",
			"imageFormat":               "2",
			"encrypted":                 "true",
			"encryptionKMSID":           encryptionServiceName,
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}
	if disableKeyRotation {
		AddAnnotation(sc, defaults.KeyRotationEnableAnnotation, "false")
	}
	return sc
}

func NewDefaultNonResilientRbdStorageClass(
	clusterID,
	topologyConstrainedPools,
	provisionerSecret,
	nodeSecret,
	namespace,
	storageId string,
	disableKeyRotation bool,
) *storagev1.StorageClass {

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Ceph Non Resilient Pools : Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				"reclaimspace.csiaddons.openshift.io/schedule": "@weekly",
			},
			Labels: map[string]string{},
		},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Provisioner:          RbdDriverName,
		VolumeBindingMode:    ptr.To(storagev1.VolumeBindingWaitForFirstConsumer),
		Parameters: map[string]string{
			"clusterID":                 clusterID,
			"topologyConstrainedPools":  topologyConstrainedPools,
			"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
			"csi.storage.k8s.io/fstype": "ext4",
			"imageFormat":               "2",
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}

	if disableKeyRotation {
		AddAnnotation(sc, defaults.KeyRotationEnableAnnotation, "false")
	}
	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultCephFsStorageClass(
	clusterID,
	fsName,
	provisionerSecret,
	nodeSecret,
	namespace,
	storageId string,
) *storagev1.StorageClass {

	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Filesystem volumes",
			},
			Labels: map[string]string{},
		},
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Provisioner:          CephFSDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"fsName":    fsName,
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}

	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultOBCStorageClass(
	objectStoreNameSpace,
	objectStoreName string,
) *storagev1.StorageClass {
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides Object Bucket Claims (OBCs)",
			},
		},
		Provisioner:   ObcDriverName,
		ReclaimPolicy: ptr.To(corev1.PersistentVolumeReclaimDelete),
		Parameters: map[string]string{
			"objectStoreNamespace": objectStoreNameSpace,
			"region":               "us-east-1",
			"objectStoreName":      objectStoreName,
		},
	}
	return sc
}

func NewDefaultNFSStorageClass(
	clusterID,
	nfsCluster,
	fsName,
	server,
	provisionerSecret,
	nodeSecret,
	namespace string,
) *storagev1.StorageClass {
	sc := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Filesystem volumes",
			},
		},
		Provisioner:          NfsDriverName,
		ReclaimPolicy:        ptr.To(corev1.PersistentVolumeReclaimDelete),
		AllowVolumeExpansion: ptr.To(true),
		Parameters: map[string]string{
			"clusterID":        clusterID,
			"nfsCluster":       nfsCluster,
			"fsName":           fsName,
			"server":           server,
			"volumeNamePrefix": "nfs-export-",
			"csi.storage.k8s.io/provisioner-secret-name":            provisionerSecret,
			"csi.storage.k8s.io/provisioner-secret-namespace":       namespace,
			"csi.storage.k8s.io/node-stage-secret-name":             nodeSecret,
			"csi.storage.k8s.io/node-stage-secret-namespace":        namespace,
			"csi.storage.k8s.io/controller-expand-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/controller-expand-secret-namespace": namespace,
		},
	}

	return sc
}

func StorageClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	storageClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	cephFsStorageId,
	nfsStorageId string,
) (*storagev1.StorageClass, error) {
	storageClass := &storagev1.StorageClass{}
	storageClass.Name = storageClassName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(storageClass), storageClass); err != nil {
		return nil, err
	}
	clientProfileName := ""
	provisionerSecretName := ""
	nodeSecretName := ""
	storageId := ""
	operatorNamespace := consumer.Status.Client.OperatorNamespace
	switch storageClass.Provisioner {
	case RbdDriverName:
		clientProfileName = consumerConfig.GetRbdClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiRbdProvisionerSecretName()
		nodeSecretName = consumerConfig.GetCsiRbdNodeSecretName()
		storageId = rbdStorageId
	case CephFSDriverName:
		clientProfileName = consumerConfig.GetCephFsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiCephFsProvisionerSecretName()
		nodeSecretName = consumerConfig.GetCsiCephFsNodeSecretName()
		storageId = cephFsStorageId
	case NfsDriverName:
		clientProfileName = consumerConfig.GetNfsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiNfsProvisionerSecretName()
		nodeSecretName = consumerConfig.GetCsiNfsNodeSecretName()
		storageId = nfsStorageId
	default:
		return nil, UnsupportedProvisioner
	}

	params := storageClass.Parameters
	if params == nil {
		params = map[string]string{}
		storageClass.Parameters = params
	}
	params["clusterID"] = clientProfileName
	params["csi.storage.k8s.io/provisioner-secret-name"] = provisionerSecretName
	params["csi.storage.k8s.io/provisioner-secret-namespace"] = operatorNamespace
	params["csi.storage.k8s.io/node-stage-secret-name"] = nodeSecretName
	params["csi.storage.k8s.io/node-stage-secret-namespace"] = operatorNamespace
	params["csi.storage.k8s.io/controller-expand-secret-name"] = provisionerSecretName
	params["csi.storage.k8s.io/controller-expand-secret-namespace"] = operatorNamespace
	AddLabel(storageClass, storageIdLabelKey, storageId)
	return storageClass, nil
}
