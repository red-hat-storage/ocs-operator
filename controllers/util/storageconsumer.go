package util

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Reserved RadosNamespaceName for internal use and their representation at different layes
	ImplicitRbdRadosNamespaceName = "<implicit>"
	TicketAnnotation              = "ocs.openshift.io/provider-onboarding-ticket"

	// Constants for ConfigMap keys
	rbdRadosNamespaceKey            = "rbd-rados-ns"
	subVolumeGroupKey               = "cephfs-subvolumegroup"
	subVolumeGroupRadosNamespaceKey = "cephfs-subvolumegroup-rados-ns"
	csiRbdProvisionerSecretKey      = "csi-rbd-provisioner-secret"
	csiRbdNodeSecretKey             = "csi-rbd-node-secret"
	csiCephFsProvisionerSecretKey   = "csi-cephfs-provisioner-secret"
	csiCephFsNodeSecretKey          = "csi-cephfs-node-secret"
	csiNfsProvisionerSecretKey      = "csi-nfs-provisioner-secret"
	csiNfsNodeSecretKey             = "csi-nfs-node-secret"
	rbdClientProfileKey             = "csiop-rbd-client-profile"
	cephFsClientProfileKey          = "csiop-cephfs-client-profile"
	nfsClientProfileKey             = "csiop-nfs-client-profile"
)

type AvailableServices struct {
	Rbd    bool
	CephFs bool
	Nfs    bool
	Mcg    bool
}

type StorageConsumerResources interface {
	// Getters
	GetRbdRadosNamespaceName() string
	GetSubVolumeGroupName() string
	GetSubVolumeGroupRadosNamespaceName() string
	GetCsiRbdProvisionerSecretName() string
	GetCsiRbdNodeSecretName() string
	GetCsiCephFsProvisionerSecretName() string
	GetCsiCephFsNodeSecretName() string
	GetCsiNfsProvisionerSecretName() string
	GetCsiNfsNodeSecretName() string
	GetRbdClientProfileName() string
	GetCephFsClientProfileName() string
	GetNfsClientProfileName() string

	// Setters
	SetRbdRadosNamespaceName(string)
	SetSubVolumeGroupName(string)
	SetSubVolumeGroupRadosNamespaceName(string)
	SetCsiRbdProvisionerSecretName(string)
	SetCsiRbdNodeSecretName(string)
	SetCsiCephFsProvisionerSecretName(string)
	SetCsiCephFsNodeSecretName(string)
	SetCsiNfsProvisionerSecretName(string)
	SetCsiNfsNodeSecretName(string)
	SetRbdClientProfileName(string)
	SetCephFsClientProfileName(string)
	SetNfsClientProfileName(string)

	ReplaceRbdRadosNamespaceName(string)
	ReplaceSubVolumeGroupName(string)
	ReplaceSubVolumeGroupRadosNamespaceName(string)
	ReplaceCsiRbdProvisionerSecretName(string)
	ReplaceCsiRbdNodeSecretName(string)
	ReplaceCsiCephFsProvisionerSecretName(string)
	ReplaceCsiCephFsNodeSecretName(string)
	ReplaceCsiNfsProvisionerSecretName(string)
	ReplaceCsiNfsNodeSecretName(string)
	ReplaceRbdClientProfileName(string)
	ReplaceCephFsClientProfileName(string)
	ReplaceNfsClientProfileName(string)
}

type storageConsumerResourceMapWrapper struct {
	data map[string]string
}

func WrapStorageConsumerResourceMap(data map[string]string) StorageConsumerResources {
	return &storageConsumerResourceMapWrapper{data: data}
}

// Getters
func (wrapper storageConsumerResourceMapWrapper) GetRbdRadosNamespaceName() string {
	return wrapper.data[rbdRadosNamespaceKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetSubVolumeGroupName() string {
	return wrapper.data[subVolumeGroupKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetSubVolumeGroupRadosNamespaceName() string {
	return wrapper.data[subVolumeGroupRadosNamespaceKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiRbdProvisionerSecretName() string {
	return wrapper.data[csiRbdProvisionerSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiRbdNodeSecretName() string {
	return wrapper.data[csiRbdNodeSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiCephFsProvisionerSecretName() string {
	return wrapper.data[csiCephFsProvisionerSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiCephFsNodeSecretName() string {
	return wrapper.data[csiCephFsNodeSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiNfsProvisionerSecretName() string {
	return wrapper.data[csiNfsProvisionerSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiNfsNodeSecretName() string {
	return wrapper.data[csiNfsNodeSecretKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetRbdClientProfileName() string {
	return wrapper.data[rbdClientProfileKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCephFsClientProfileName() string {
	return wrapper.data[cephFsClientProfileKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetNfsClientProfileName() string {
	return wrapper.data[nfsClientProfileKey]
}

// Setters
func (wrapper storageConsumerResourceMapWrapper) SetRbdRadosNamespaceName(name string) {
	wrapper.data[rbdRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupName(name string) {
	wrapper.data[subVolumeGroupKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupRadosNamespaceName(name string) {
	wrapper.data[subVolumeGroupRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdProvisionerSecretName(name string) {
	wrapper.data[csiRbdProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdNodeSecretName(name string) {
	wrapper.data[csiRbdNodeSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsProvisionerSecretName(name string) {
	wrapper.data[csiCephFsProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsNodeSecretName(name string) {
	wrapper.data[csiCephFsNodeSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsProvisionerSecretName(name string) {
	wrapper.data[csiNfsProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsNodeSecretName(name string) {
	wrapper.data[csiNfsNodeSecretKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetRbdClientProfileName(name string) {
	wrapper.data[rbdClientProfileKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCephFsClientProfileName(name string) {
	wrapper.data[cephFsClientProfileKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetNfsClientProfileName(name string) {
	wrapper.data[nfsClientProfileKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) replaceIfExist(key, value string) {
	if oldValue, exist := wrapper.data[key]; exist && oldValue != value {
		wrapper.data[key] = value
	}
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceRbdRadosNamespaceName(name string) {
	wrapper.replaceIfExist(rbdRadosNamespaceKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceSubVolumeGroupName(name string) {
	wrapper.replaceIfExist(subVolumeGroupKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceSubVolumeGroupRadosNamespaceName(name string) {
	wrapper.replaceIfExist(subVolumeGroupRadosNamespaceKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiRbdProvisionerSecretName(name string) {
	wrapper.replaceIfExist(csiRbdProvisionerSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiRbdNodeSecretName(name string) {
	wrapper.replaceIfExist(csiRbdNodeSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiCephFsProvisionerSecretName(name string) {
	wrapper.replaceIfExist(csiCephFsProvisionerSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiCephFsNodeSecretName(name string) {
	wrapper.replaceIfExist(csiCephFsNodeSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiNfsProvisionerSecretName(name string) {
	wrapper.replaceIfExist(csiNfsProvisionerSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiNfsNodeSecretName(name string) {
	wrapper.replaceIfExist(csiNfsNodeSecretKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceRbdClientProfileName(name string) {
	wrapper.replaceIfExist(rbdClientProfileKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCephFsClientProfileName(name string) {
	wrapper.replaceIfExist(cephFsClientProfileKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceNfsClientProfileName(name string) {
	wrapper.replaceIfExist(nfsClientProfileKey, name)
}

func GetAvailableServices(ctx context.Context, kubeClient client.Client, storageCluster *ocsv1.StorageCluster) (*AvailableServices, error) {
	availableServices := &AvailableServices{}

	// always enable rbd because internal pools are using it
	availableServices.Rbd = true

	// cephFs - get the default filesystem
	cephFs := &rookCephv1.CephFilesystem{}
	cephFs.Name = GenerateNameForCephFilesystem(storageCluster.Name)
	cephFs.Namespace = storageCluster.Namespace
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(cephFs), cephFs); client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("failed to get CephFilesystem: %v", err)
	}
	availableServices.CephFs = cephFs.UID != ""

	// nfs - get the default nfs obj
	cephNfs := &rookCephv1.CephNFS{}
	cephNfs.Name = GenerateNameForCephNFS(storageCluster)
	cephNfs.Namespace = storageCluster.Namespace
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(cephNfs), cephNfs); client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("failed to get CephNFS: %v", err)
	}
	availableServices.Nfs = cephNfs.UID != ""

	// noobaa - get the default noobaa obj
	noobaa := &nbv1.NooBaa{}
	noobaa.Name = "noobaa"
	noobaa.Namespace = storageCluster.Namespace
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(noobaa), noobaa); client.IgnoreNotFound(err) != nil {
		return nil, fmt.Errorf("failed to get NooBaa: %v", err)
	}
	availableServices.Mcg = noobaa.UID != ""

	return availableServices, nil
}

func GetStorageConsumerDefaultResourceNames(
	storageConsumerName,
	storageConsumerUid string,
	availableServices *AvailableServices,
) map[string]string {
	defaults := map[string]string{}
	resourceNamesMap := WrapStorageConsumerResourceMap(defaults)
	if availableServices.Rbd {
		resourceNamesMap.SetRbdRadosNamespaceName(storageConsumerName)
		resourceNamesMap.SetRbdClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiRbdNodeSecretName(fmt.Sprintf("rbd-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiRbdProvisionerSecretName(fmt.Sprintf("rbd-provisioner-%s", storageConsumerUid))
	}
	if availableServices.CephFs {
		resourceNamesMap.SetSubVolumeGroupName(storageConsumerName)
		resourceNamesMap.SetSubVolumeGroupRadosNamespaceName(storageConsumerName)
		resourceNamesMap.SetCephFsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiCephFsNodeSecretName(fmt.Sprintf("cephfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiCephFsProvisionerSecretName(fmt.Sprintf("cephfs-provisioner-%s", storageConsumerUid))
	}
	if availableServices.Nfs {
		resourceNamesMap.SetNfsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiNfsNodeSecretName(fmt.Sprintf("nfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiNfsProvisionerSecretName(fmt.Sprintf("nfs-provisioner-%s", storageConsumerUid))
	}
	return defaults
}
