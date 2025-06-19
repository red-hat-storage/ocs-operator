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
	ImplicitRbdRadosNamespaceName          = "<implicit>"
	TicketAnnotation                       = "ocs.openshift.io/provider-onboarding-ticket"
	AnnotationNonResilientPoolsTopologyKey = "ocs.openshift.io/non-resilient-pools-topology-key"

	// Constants for ConfigMap keys
	rbdRadosNamespaceKey            = "rbd-rados-ns"
	subVolumeGroupKey               = "cephfs-subvolumegroup"
	subVolumeGroupRadosNamespaceKey = "cephfs-subvolumegroup-rados-ns"
	csiRbdProvisionerCephUserKey    = "csi-rbd-provisioner-ceph-user"
	csiRbdNodeCephUserKey           = "csi-rbd-node-ceph-user"
	csiCephFsProvisionerCephUserKey = "csi-cephfs-provisioner-ceph-user"
	csiCephFsNodeCephUserKey        = "csi-cephfs-node-ceph-user"
	csiNfsProvisionerCephUserKey    = "csi-nfs-provisioner-ceph-user"
	csiNfsNodeCephUserKey           = "csi-nfs-node-ceph-user"
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
	GetCsiRbdProvisionerCephUserName() string
	GetCsiRbdNodeCephUserName() string
	GetCsiCephFsProvisionerCephUserName() string
	GetCsiCephFsNodeCephUserName() string
	GetCsiNfsProvisionerCephUserName() string
	GetCsiNfsNodeCephUserName() string
	GetRbdClientProfileName() string
	GetCephFsClientProfileName() string
	GetNfsClientProfileName() string

	// Setters
	SetRbdRadosNamespaceName(string)
	SetSubVolumeGroupName(string)
	SetSubVolumeGroupRadosNamespaceName(string)
	SetCsiRbdProvisionerCephUserName(string)
	SetCsiRbdNodeCephUserName(string)
	SetCsiCephFsProvisionerCephUserName(string)
	SetCsiCephFsNodeCephUserName(string)
	SetCsiNfsProvisionerCephUserName(string)
	SetCsiNfsNodeCephUserName(string)
	SetRbdClientProfileName(string)
	SetCephFsClientProfileName(string)
	SetNfsClientProfileName(string)

	ReplaceRbdRadosNamespaceName(string)
	ReplaceSubVolumeGroupName(string)
	ReplaceSubVolumeGroupRadosNamespaceName(string)
	ReplaceCsiRbdProvisionerCephUserName(string)
	ReplaceCsiRbdNodeCephUserName(string)
	ReplaceCsiCephFsProvisionerCephUserName(string)
	ReplaceCsiCephFsNodeCephUserName(string)
	ReplaceCsiNfsProvisionerCephUserName(string)
	ReplaceCsiNfsNodeCephUserName(string)
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

func (wrapper storageConsumerResourceMapWrapper) GetCsiRbdProvisionerCephUserName() string {
	return wrapper.data[csiRbdProvisionerCephUserKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiRbdNodeCephUserName() string {
	return wrapper.data[csiRbdNodeCephUserKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiCephFsProvisionerCephUserName() string {
	return wrapper.data[csiCephFsProvisionerCephUserKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiCephFsNodeCephUserName() string {
	return wrapper.data[csiCephFsNodeCephUserKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiNfsProvisionerCephUserName() string {
	return wrapper.data[csiNfsProvisionerCephUserKey]
}

func (wrapper storageConsumerResourceMapWrapper) GetCsiNfsNodeCephUserName() string {
	return wrapper.data[csiNfsNodeCephUserKey]
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
func (wrapper storageConsumerResourceMapWrapper) setIfDoesntExist(key, value string) {
	if _, exist := wrapper.data[key]; !exist {
		wrapper.data[key] = value
	}
}

func (wrapper storageConsumerResourceMapWrapper) SetRbdRadosNamespaceName(name string) {
	wrapper.setIfDoesntExist(rbdRadosNamespaceKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupName(name string) {
	wrapper.setIfDoesntExist(subVolumeGroupKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupRadosNamespaceName(name string) {
	wrapper.setIfDoesntExist(subVolumeGroupRadosNamespaceKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdProvisionerCephUserName(name string) {
	wrapper.setIfDoesntExist(csiRbdProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdNodeCephUserName(name string) {
	wrapper.setIfDoesntExist(csiRbdNodeCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsProvisionerCephUserName(name string) {
	wrapper.setIfDoesntExist(csiCephFsProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsNodeCephUserName(name string) {
	wrapper.setIfDoesntExist(csiCephFsNodeCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsProvisionerCephUserName(name string) {
	wrapper.setIfDoesntExist(csiNfsProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsNodeCephUserName(name string) {
	wrapper.setIfDoesntExist(csiNfsNodeCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetRbdClientProfileName(name string) {
	wrapper.setIfDoesntExist(rbdClientProfileKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetCephFsClientProfileName(name string) {
	wrapper.setIfDoesntExist(cephFsClientProfileKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) SetNfsClientProfileName(name string) {
	wrapper.setIfDoesntExist(nfsClientProfileKey, name)
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

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiRbdProvisionerCephUserName(name string) {
	wrapper.replaceIfExist(csiRbdProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiRbdNodeCephUserName(name string) {
	wrapper.replaceIfExist(csiRbdNodeCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiCephFsProvisionerCephUserName(name string) {
	wrapper.replaceIfExist(csiCephFsProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiCephFsNodeCephUserName(name string) {
	wrapper.replaceIfExist(csiCephFsNodeCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiNfsProvisionerCephUserName(name string) {
	wrapper.replaceIfExist(csiNfsProvisionerCephUserKey, name)
}

func (wrapper storageConsumerResourceMapWrapper) ReplaceCsiNfsNodeCephUserName(name string) {
	wrapper.replaceIfExist(csiNfsNodeCephUserKey, name)
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
		resourceNamesMap.SetCsiRbdNodeCephUserName(fmt.Sprintf("rbd-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiRbdProvisionerCephUserName(fmt.Sprintf("rbd-provisioner-%s", storageConsumerUid))
	}
	if availableServices.CephFs {
		resourceNamesMap.SetSubVolumeGroupName(storageConsumerName)
		resourceNamesMap.SetSubVolumeGroupRadosNamespaceName(storageConsumerName)
		resourceNamesMap.SetCephFsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiCephFsNodeCephUserName(fmt.Sprintf("cephfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiCephFsProvisionerCephUserName(fmt.Sprintf("cephfs-provisioner-%s", storageConsumerUid))
	}
	if availableServices.Nfs {
		resourceNamesMap.SetNfsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiNfsNodeCephUserName(fmt.Sprintf("nfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiNfsProvisionerCephUserName(fmt.Sprintf("nfs-provisioner-%s", storageConsumerUid))
	}
	return defaults
}
