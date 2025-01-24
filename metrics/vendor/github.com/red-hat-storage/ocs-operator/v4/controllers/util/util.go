package util

import (
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	if schedule == "" {
		// default schedule
		schedule = "@weekly"
	}

	if sc.Spec.Encryption.KeyRotation.Enable == nil {
		if IsClusterOrDeviceSetEncrypted(sc) && !sc.Spec.Encryption.KeyManagementService.Enable {
			// use key-rotation by default if cluster-wide encryption/any deviceSet encryption is opted without KMS & "enable" spec is missing
			return true, schedule
		}
		return false, schedule
	}
	return *sc.Spec.Encryption.KeyRotation.Enable, schedule
}

// Find returns the first entry matching the function "f" or else return nil
func Find[T any](list []T, f func(item *T) bool) *T {
	for idx := range list {
		ele := &list[idx]
		if f(ele) {
			return ele
		}
	}
	return nil
}

func AddAnnotation(obj metav1.Object, key string, value string) bool {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}
	if oldValue, exist := annotations[key]; !exist || oldValue != value {
		annotations[key] = value
		return true
	}
	return false
}

func AddLabel(obj metav1.Object, key string, value string) bool {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
		obj.SetLabels(labels)
	}
	if oldValue, exist := labels[key]; !exist || oldValue != value {
		labels[key] = value
		return true
	}
	return false
}

func CalculateMD5Hash(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		errStr := fmt.Errorf("failed to marshal for %#v", value)
		panic(errStr)
	}
	hash := md5.Sum(data)
	return hex.EncodeToString(hash[:])
}

/*
fnv64a is a 64-bit non-cryptographic hash algorithm with a low collision and a high distribution rate.
https://en.wikipedia.org/wiki/Fowler%E2%80%93Noll%E2%80%93Vo_hash_function
*/
func FnvHash(s string) uint32 {
	h := fnv.New32a()
	_, err := h.Write([]byte(s))
	if err != nil {
		return 0
	}
	return h.Sum32()
}

func AssertEqual[T comparable](actual T, expected T, exitCode int) {
	if actual != expected {
		os.Exit(exitCode)
	}
}

func IsClusterOrDeviceSetEncrypted(sc *ocsv1.StorageCluster) bool {
	// If cluster-wide encryption is enabled
	if sc.Spec.Encryption.Enable || sc.Spec.Encryption.ClusterWide {
		return true
	}

	// If any device set is encrypted
	for _, deviceSet := range sc.Spec.StorageDeviceSets {
		if deviceSet.Encrypted != nil && *deviceSet.Encrypted {
			return true
		}
	}

	return false
}

func GenerateCephClientSecretName(storageType, userType, storageClusterUid string) string {
	return fmt.Sprintf("%s-%s-%s", storageType, userType, storageClusterUid)
}
