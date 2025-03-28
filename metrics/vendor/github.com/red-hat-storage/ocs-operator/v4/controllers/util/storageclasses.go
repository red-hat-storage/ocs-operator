package util

import (
	"fmt"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"
	ramenDRStorageIDLabelKey      = "ramendr.openshift.io/storageid"
)

func GenerateNameForCephBlockPoolSC(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephBlockPools.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephBlockPools.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd", storageCluster.Name)
}

func GenerateNameForCephBlockPoolVirtualizationSC(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephBlockPools.VirtualizationStorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephBlockPools.VirtualizationStorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd-virtualization", storageCluster.Name)
}

func GenerateNameForNonResilientCephBlockPoolSC(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephNonResilientPools.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephNonResilientPools.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-non-resilient-rbd", storageCluster.Name)
}

func GenerateNameForCephFilesystemSC(storageCluster *ocsv1.StorageCluster) string {
	if storageCluster.Spec.ManagedResources.CephFilesystems.StorageClassName != "" {
		return storageCluster.Spec.ManagedResources.CephFilesystems.StorageClassName
	}
	return fmt.Sprintf("%s-cephfs", storageCluster.Name)
}

func GenerateNameForCephRgwSC(initData *ocsv1.StorageCluster) string {
	if initData.Spec.ManagedResources.CephObjectStores.StorageClassName != "" {
		return initData.Spec.ManagedResources.CephObjectStores.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rgw", initData.Name)
}

func GenerateNameForEncryptedCephBlockPoolSC(initData *ocsv1.StorageCluster) string {
	if initData.Spec.Encryption.StorageClassName != "" {
		return initData.Spec.Encryption.StorageClassName
	}
	return fmt.Sprintf("%s-ceph-rbd-encrypted", initData.Name)
}

func GenerateNameForCephNetworkFilesystemSC(initData *ocsv1.StorageCluster) string {
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
	encryptionServiceName,
	drStorageID string,
	isDefaultStorageClass,
	disableKeyRotation,
	virtStorageClass bool,
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
	if disableKeyRotation {
		AddAnnotation(sc, defaults.KeyRotationEnableAnnotation, "false")
	}
	if encryptionServiceName != "" {
		AddAnnotation(sc, "cdi.kubevirt.io/clone-strategy", "copy")
		sc.Parameters["encrypted"] = "true"
		sc.Parameters["encryptionKMSID"] = encryptionServiceName
	}
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	if virtStorageClass {
		AddAnnotation(sc, "description", "Provides RWO and RWX Block volumes suitable for Virtual Machine disks")
		AddAnnotation(sc, "storageclass.kubevirt.io/is-default-virt-class", "true")
		sc.Parameters["mounter"] = "rbd"
		sc.Parameters["mapOptions"] = "krbd:rxbounce"
	}
	return sc
}

func NewDefaultVirtRbdStorageClass(
	clusterID,
	poolName,
	provisionerSecret,
	nodeSecret,
	namespace,
	encryptionServiceName,
	drStorageID string,
	isDefaultStorageClass,
	disableKeyRotation bool,
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
	if disableKeyRotation {
		AddAnnotation(sc, defaults.KeyRotationEnableAnnotation, "false")
	}
	if encryptionServiceName != "" {
		AddAnnotation(sc, "cdi.kubevirt.io/clone-strategy", "copy")
		sc.Parameters["encrypted"] = "true"
		sc.Parameters["encryptionKMSID"] = encryptionServiceName
	}
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	return sc
}

func NewDefaultNonResilientRbdStorageClass(
	clusterID,
	topologyConstrainedPools,
	provisionerSecret,
	nodeSecret,
	namespace,
	drStorageID string,
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
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	return sc
}

func NewDefaultCephFsStorageClass(
	clusterID,
	fsName,
	provisionerSecret,
	nodeSecret,
	namespace,
	drStorageID string,
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

	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
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
