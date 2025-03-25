package util

import (
	corev1 "k8s.io/api/core/v1"
)

// Constants for ConfigMap keys
const (
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
}

type storageConsumerResourceConfigMapWrapper struct {
	*corev1.ConfigMap
}

func WrapStorageConsumerResourceConfigMap(cm *corev1.ConfigMap) StorageConsumerResources {
	return &storageConsumerResourceConfigMapWrapper{cm}
}

// Getters
func (wrapper storageConsumerResourceConfigMapWrapper) GetRbdRadosNamespaceName() string {
	return wrapper.Data[rbdRadosNamespaceKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetSubVolumeGroupName() string {
	return wrapper.Data[subVolumeGroupKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetSubVolumeGroupRadosNamespaceName() string {
	return wrapper.Data[subVolumeGroupRadosNamespaceKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiRbdProvisionerSecretName() string {
	return wrapper.Data[csiRbdProvisionerSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiRbdNodeSecretName() string {
	return wrapper.Data[csiRbdNodeSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiCephFsProvisionerSecretName() string {
	return wrapper.Data[csiCephFsProvisionerSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiCephFsNodeSecretName() string {
	return wrapper.Data[csiCephFsNodeSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiNfsProvisionerSecretName() string {
	return wrapper.Data[csiNfsProvisionerSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCsiNfsNodeSecretName() string {
	return wrapper.Data[csiNfsNodeSecretKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetRbdClientProfileName() string {
	return wrapper.Data[rbdClientProfileKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetCephFsClientProfileName() string {
	return wrapper.Data[cephFsClientProfileKey]
}

func (wrapper storageConsumerResourceConfigMapWrapper) GetNfsClientProfileName() string {
	return wrapper.Data[nfsClientProfileKey]
}

// Setters
func (wrapper storageConsumerResourceConfigMapWrapper) SetRbdRadosNamespaceName(name string) {
	wrapper.Data[rbdRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetSubVolumeGroupName(name string) {
	wrapper.Data[subVolumeGroupKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetSubVolumeGroupRadosNamespaceName(name string) {
	wrapper.Data[subVolumeGroupRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiRbdProvisionerSecretName(name string) {
	wrapper.Data[csiRbdProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiRbdNodeSecretName(name string) {
	wrapper.Data[csiRbdNodeSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiCephFsProvisionerSecretName(name string) {
	wrapper.Data[csiCephFsProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiCephFsNodeSecretName(name string) {
	wrapper.Data[csiCephFsNodeSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiNfsProvisionerSecretName(name string) {
	wrapper.Data[csiNfsProvisionerSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCsiNfsNodeSecretName(name string) {
	wrapper.Data[csiNfsNodeSecretKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetRbdClientProfileName(name string) {
	wrapper.Data[rbdClientProfileKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetCephFsClientProfileName(name string) {
	wrapper.Data[cephFsClientProfileKey] = name
}

func (wrapper storageConsumerResourceConfigMapWrapper) SetNfsClientProfileName(name string) {
	wrapper.Data[nfsClientProfileKey] = name
}
