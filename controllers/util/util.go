package util

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

func RemoveDuplicatesFromStringSlice(slice []string) []string {
	keys := make(map[string]bool)
	list := []string{}
	for _, entry := range slice {
		if _, ok := keys[entry]; !ok {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	return list
}

func DetectDuplicateInStringSlice(slice []string) bool {
	keys := make(map[string]bool)
	for _, entry := range slice {
		if _, ok := keys[entry]; ok {
			return true
		}
		keys[entry] = true
	}
	return false
}

func GetKeyRotationSpec(sc *ocsv1.StorageCluster) (bool, string) {
	schedule := sc.Spec.Encryption.KeyRotation.Schedule
	if (sc.Spec.Encryption.Enable || sc.Spec.Encryption.ClusterWide) && !sc.Spec.Encryption.KeyManagementService.Enable {
		if schedule == "" {
			// default schedule
			schedule = "@weekly"
		}
		// use key-rotation by default if cluster-wide encryption is opted without KMS
		return true, schedule
	}
	return sc.Spec.Encryption.KeyRotation.Enable, schedule
}
