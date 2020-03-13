package storagecluster

import (
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
)

func generateNameForExternalCephCluster(initData *ocsv1.StorageCluster) string {
	return generateNameForCephClusterFromString(fmt.Sprintf("%s-external", initData.Name))
}

func generateNameForCephCluster(initData *ocsv1.StorageCluster) string {
	clusterName := ""
	// if 'ExternalStorage' is enabled, change the cluster name accordingly
	if initData.Spec.ExternalStorage.Enable {
		clusterName = generateNameForExternalCephCluster(initData)
	} else {
		clusterName = generateNameForCephClusterFromString(initData.Name)
	}
	return clusterName
}

func generateNameForCephClusterFromString(name string) string {
	return fmt.Sprintf("%s-cephcluster", name)
}

func generateNameForCephFilesystem(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephfilesystem", initData.Name)
}

func generateNameForCephObjectStoreUser(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func generateNameForCephBlockPool(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephblockpool", initData.Name)
}

func generateNameForCephObjectStore(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephobjectstore", initData.Name)
}

func generateNameForCephFilesystemSC(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-cephfs", initData.Name)
}

func generateNameForCephBlockPoolSC(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-ceph-rbd", initData.Name)
}
