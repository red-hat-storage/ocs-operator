package util

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
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
func (wrapper storageConsumerResourceMapWrapper) SetRbdRadosNamespaceName(name string) {
	wrapper.data[rbdRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupName(name string) {
	wrapper.data[subVolumeGroupKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetSubVolumeGroupRadosNamespaceName(name string) {
	wrapper.data[subVolumeGroupRadosNamespaceKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdProvisionerCephUserName(name string) {
	wrapper.data[csiRbdProvisionerCephUserKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiRbdNodeCephUserName(name string) {
	wrapper.data[csiRbdNodeCephUserKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsProvisionerCephUserName(name string) {
	wrapper.data[csiCephFsProvisionerCephUserKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiCephFsNodeCephUserName(name string) {
	wrapper.data[csiCephFsNodeCephUserKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsProvisionerCephUserName(name string) {
	wrapper.data[csiNfsProvisionerCephUserKey] = name
}

func (wrapper storageConsumerResourceMapWrapper) SetCsiNfsNodeCephUserName(name string) {
	wrapper.data[csiNfsNodeCephUserKey] = name
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
		resourceNamesMap.SetCsiRbdNodeCephUserName(fmt.Sprintf("csi-rbd-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiRbdProvisionerCephUserName(fmt.Sprintf("csi-rbd-provisioner-%s", storageConsumerUid))
	}
	if availableServices.CephFs {
		resourceNamesMap.SetSubVolumeGroupName(storageConsumerName)
		resourceNamesMap.SetSubVolumeGroupRadosNamespaceName(storageConsumerName)
		resourceNamesMap.SetCephFsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiCephFsNodeCephUserName(fmt.Sprintf("csi-cephfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiCephFsProvisionerCephUserName(fmt.Sprintf("csi-cephfs-provisioner-%s", storageConsumerUid))
	}
	if availableServices.Nfs {
		resourceNamesMap.SetNfsClientProfileName(storageConsumerUid)
		resourceNamesMap.SetCsiNfsNodeCephUserName(fmt.Sprintf("csi-nfs-node-%s", storageConsumerUid))
		resourceNamesMap.SetCsiNfsProvisionerCephUserName(fmt.Sprintf("csi-nfs-provisioner-%s", storageConsumerUid))
	}
	return defaults
}

// These methods are for backward compatibility for provider mode upgraded cluster

func FillBackwardCompatibleConsumerConfigValues(
	storageCluster *ocsv1.StorageCluster,
	storageConsumerUID string,
	resourceMap StorageConsumerResources,
) {

	// For Provider Mode we supported creating one rbdClaim where we generate clientProfile, secret and rns name using
	// consumer UID and Storage Claim name
	// The name of StorageClass is the same as name of storageClaim in provider mode
	rbdClaimName := GenerateNameForCephBlockPoolStorageClass(storageCluster)
	rbdClaimMd5Sum := md5.Sum([]byte(rbdClaimName))
	rbdClientProfile := hex.EncodeToString(rbdClaimMd5Sum[:])
	rbdStorageRequestHash := getStorageRequestHash(storageConsumerUID, rbdClaimName)
	rbdNodeSecretName := storageClaimCephCsiSecretName("node", rbdStorageRequestHash)
	rbdProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", rbdStorageRequestHash)
	rbdStorageRequestName := GetStorageRequestName(storageConsumerUID, rbdClaimName)
	rbdStorageRequestMd5Sum := md5.Sum([]byte(rbdStorageRequestName))
	rnsName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(rbdStorageRequestMd5Sum[:16]))

	// For Provider Mode we supported creating one cephFs claim where we generate clientProfile, secret and svg name using
	// consumer UID and Storage Claim name
	// The name of StorageClass is the same as name of storageClaim in provider mode
	cephFsClaimName := GenerateNameForCephFilesystemStorageClass(storageCluster)
	cephFsClaimMd5Sum := md5.Sum([]byte(cephFsClaimName))
	cephFSClientProfile := hex.EncodeToString(cephFsClaimMd5Sum[:])
	cephFsStorageRequestHash := getStorageRequestHash(storageConsumerUID, cephFsClaimName)
	cephFsNodeSecretName := storageClaimCephCsiSecretName("node", cephFsStorageRequestHash)
	cephFsProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", cephFsStorageRequestHash)
	cephFsStorageRequestName := GetStorageRequestName(storageConsumerUID, cephFsClaimName)
	cephFsStorageRequestMd5Sum := md5.Sum([]byte(cephFsStorageRequestName))
	svgName := fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsStorageRequestMd5Sum[:16]))

	resourceMap.ReplaceRbdRadosNamespaceName(rnsName)
	resourceMap.ReplaceSubVolumeGroupName(svgName)
	resourceMap.ReplaceSubVolumeGroupRadosNamespaceName("csi")
	resourceMap.ReplaceRbdClientProfileName(rbdClientProfile)
	resourceMap.ReplaceCephFsClientProfileName(cephFSClientProfile)
	resourceMap.ReplaceCsiRbdNodeCephUserName(rbdNodeSecretName)
	resourceMap.ReplaceCsiRbdProvisionerCephUserName(rbdProvisionerSecretName)
	resourceMap.ReplaceCsiCephFsNodeCephUserName(cephFsNodeSecretName)
	resourceMap.ReplaceCsiCephFsProvisionerCephUserName(cephFsProvisionerSecretName)

}

// GetStorageRequestName generates a name for a StorageRequest resource.
func GetStorageRequestName(consumerUUID, storageClaimName string) string {
	return fmt.Sprintf("storagerequest-%s", getStorageRequestHash(consumerUUID, storageClaimName))
}

// getStorageRequestHash generates a hash for a StorageRequest based
// on the MD5 hash of the StorageClaim name and storageConsumer UUID.
func getStorageRequestHash(consumerUUID, storageClaimName string) string {
	s := struct {
		StorageConsumerUUID string `json:"storageConsumerUUID"`
		StorageClaimName    string `json:"storageClaimName"`
	}{
		consumerUUID,
		storageClaimName,
	}

	requestName, err := json.Marshal(s)
	if err != nil {
		panic("failed to marshal storage class request name")
	}
	md5Sum := md5.Sum(requestName)
	return hex.EncodeToString(md5Sum[:16])
}

func storageClaimCephCsiSecretName(secretType, suffix string) string {
	return fmt.Sprintf("ceph-client-%s-%s", secretType, suffix)
}
