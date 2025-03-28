package util

import (
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

func GenerateNameForCephFilesystem(storageClusterName string) string {
	return fmt.Sprintf("%s-cephfilesystem", storageClusterName)
}

func GenerateNameForCephObjectStoreUser(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func GenerateNameForCephObjectStore(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-%s", initData.Name, "cephobjectstore")
}

func GenerateNameForCephRbdMirror(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephrbdmirror", initData.Name)
}

func GenerateStorageQuotaName(storageClassName, quotaName string) string {
	return fmt.Sprintf("%s-%s", storageClassName, quotaName)
}
