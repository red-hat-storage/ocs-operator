package util

import (
	"fmt"
)

const (
	ValueNotApplicable = "N/A"

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
	noobaaAccountKey                = "noobaa-account"
)

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
	GetNoobaaAccountApplicability() string

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
	MarkNoobaaAccountNotApplicable()
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

func (wrapper storageConsumerResourceMapWrapper) GetNoobaaAccountApplicability() string {
	return wrapper.data[noobaaAccountKey]
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

func (wrapper storageConsumerResourceMapWrapper) MarkNoobaaAccountNotApplicable() {
	wrapper.data[noobaaAccountKey] = ValueNotApplicable
}

func GetStorageConsumerDefaultResourceNames(storageConsumerName, fsid string) map[string]string {

	//TODO: Revisit this to use a different method to calculate the name
	clientProfileName := CalculateMD5Hash([2]string{fsid, storageConsumerName})
	if len(clientProfileName) > 36 {
		// truncating at 36 as that is the maximum limit that csi can use in it's handle
		clientProfileName = clientProfileName[:36]
	}

	defaults := map[string]string{}
	resourceNamesMap := WrapStorageConsumerResourceMap(defaults)
	resourceNamesMap.SetRbdRadosNamespaceName(storageConsumerName)
	resourceNamesMap.SetSubVolumeGroupName(storageConsumerName)
	resourceNamesMap.SetSubVolumeGroupRadosNamespaceName(storageConsumerName)
	resourceNamesMap.SetRbdClientProfileName(clientProfileName)
	resourceNamesMap.SetCephFsClientProfileName(clientProfileName)
	resourceNamesMap.SetNfsClientProfileName(clientProfileName)
	resourceNamesMap.SetCsiRbdNodeSecretName(fmt.Sprintf("rbd-node-%s", clientProfileName))
	resourceNamesMap.SetCsiRbdProvisionerSecretName(fmt.Sprintf("rbd-provisioner-%s", clientProfileName))
	resourceNamesMap.SetCsiCephFsNodeSecretName(fmt.Sprintf("cephfs-node-%s", clientProfileName))
	resourceNamesMap.SetCsiCephFsProvisionerSecretName(fmt.Sprintf("cephfs-provisioner-%s", clientProfileName))
	resourceNamesMap.SetCsiNfsNodeSecretName(fmt.Sprintf("nfs-node-%s", clientProfileName))
	resourceNamesMap.SetCsiNfsProvisionerSecretName(fmt.Sprintf("nfs-provisioner-%s", clientProfileName))
	return defaults
}
