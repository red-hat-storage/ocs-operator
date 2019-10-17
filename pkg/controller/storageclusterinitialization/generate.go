package storageclusterinitialization

import (
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
)

func generateNameForCephFilesystem(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephfilesystem", initData.Name)
}

func generateNameForCephObjectStoreUser(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func generateNameForCephBlockPool(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephblockpool", initData.Name)
}

func generateNameForCephObjectStore(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephobjectstore", initData.Name)
}

func generateNameForCephFilesystemSC(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephfs", initData.Name)
}

func generateNameForCephBlockPoolSC(initData *ocsv1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-ceph-rbd", initData.Name)
}

// GenerateNameForCephBlockPoolSCByName generates a storage cluster's rbd storageclass name
func GenerateNameForCephBlockPoolSCByName(storageClusterName string) string {
	return fmt.Sprintf("%s-ceph-rbd", storageClusterName)
}
