package storageclusterinitialization

import (
	"fmt"

	ocsv1alpha1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1alpha1"
)

func generateNameForCephFilesystem(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephfilesystem", initData.Name)
}

func generateNameForCephObjectStoreUser(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func generateNameForCephBlockPool(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephblockpool", initData.Name)
}

func generateNameForCephObjectStore(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephobjectstore", initData.Name)
}

func generateNameForCephFilesystemSC(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-cephfs", initData.Name)
}

func generateNameForCephBlockPoolSC(initData *ocsv1alpha1.StorageClusterInitialization) string {
	return fmt.Sprintf("%s-ceph-rbd", initData.Name)
}
