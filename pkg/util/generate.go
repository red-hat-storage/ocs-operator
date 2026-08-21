package util

import (
	"encoding/json"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

func GenerateNameForCephCluster(initData *ocsv1.StorageCluster) string {
	return GenerateNameForCephClusterFromString(initData.Name)
}

func GenerateNameForCephClusterFromString(name string) string {
	return fmt.Sprintf("%s-cephcluster", name)
}

func GenerateNameForCephNFS(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephnfs", initData.Name)
}

func GenerateNameForNFSServer(initData *ocsv1.StorageCluster) string {
	nfsSpec := initData.Spec.NFS
	if nfsSpec != nil && nfsSpec.ExternalEndpoint != "" {
		return nfsSpec.ExternalEndpoint
	}
	return GenerateNameForNFSService(initData)
}

func GenerateNameForNFSService(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-service", GenerateNameForCephNFS(initData))
}

func GenerateNameForNFSServiceMonitor(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-servicemonitor", GenerateNameForCephNFS(initData))
}

func GenerateNameForCephBlockPool(storageClusterName string) string {
	return fmt.Sprintf("%s-cephblockpool", storageClusterName)
}

func GenerateNameForNonResilientCephBlockPool(storageClusterName, failureDomainValue string) string {
	return fmt.Sprintf("%s-cephblockpool-%s", storageClusterName, failureDomainValue)
}

func GenerateNameForCephNFSBlockPool(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-builtin-pool", GenerateNameForCephNFS(initData))
}

func GenerateNameForNVMeOFBlockPool(storageCluster *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-nvmeof", storageCluster.Name)
}

func GenerateNameForCephNVMeOFGateway(storageCluster *ocsv1.StorageCluster) string {
	return "nvmeof-gw"
}

func GenerateNVMeOFSubsystemNQN(namespace string) string {
	return fmt.Sprintf("nqn.2025-08.io.ceph.rook:%s", namespace)
}

type nvmeofListener struct {
	Hostname string `json:"hostname"`
}

func GenerateNVMeOFListeners(gwName string, instances int) string {
	listeners := make([]nvmeofListener, instances)
	for i := range listeners {
		suffix := string(rune('a' + i))
		listeners[i] = nvmeofListener{
			Hostname: fmt.Sprintf("rook-ceph-nvmeof-%s-%s", gwName, suffix),
		}
	}
	data, err := json.Marshal(listeners)
	if err != nil {
		panic(fmt.Sprintf("failed to marshal NVMeOF listeners: %v", err))
	}
	return string(data)
}

func GenerateNameForCephFilesystem(storageClusterName string) string {
	return fmt.Sprintf("%s-cephfilesystem", storageClusterName)
}

func GenerateNameForCephObjectStoreUser(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func GenerateNameForCephObjectStore(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-%s", initData.Name, "cephobjectstore")
}

func GenerateNameForCephObjectStoreSecureRoute(initData *ocsv1.StorageCluster) string {
	return GenerateNameForCephObjectStore(initData) + "-secure"
}

func GenerateNameForCephRbdMirror(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephrbdmirror", initData.Name)
}

func GenerateStorageQuotaName(storageClassName, quotaName string) string {
	return fmt.Sprintf("%s-%s", storageClassName, quotaName)
}
