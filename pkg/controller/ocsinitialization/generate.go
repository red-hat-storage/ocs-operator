package ocsinitialization

import (
	"fmt"

	ocsv1alpha1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1alpha1"
)

func generateNameForOCSFilesystem(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-cephfilesystem", initData.Name)
}

func generateNameForOCSObjectStoreUser(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-cephobjectstoreuser", initData.Name)
}

func generateNameForOCSBlockPool(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-cephblockpool", initData.Name)
}

func generateNameForOCSObjectStore(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-cephobjectstore", initData.Name)
}

func generateNameForOCSFilesystemSC(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-csi-cephfs", initData.Name)
}

func generateNameForOCSBlockPoolSC(initData *ocsv1alpha1.OCSInitialization) string {
	return fmt.Sprintf("%s-ceph-block", initData.Name)
}
