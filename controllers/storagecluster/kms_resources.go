package storagecluster

import (
	"context"
	"fmt"
	"time"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"k8s.io/apimachinery/pkg/types"
)

const (
	// KMSConfigMapName is the name configmap which has KMS config details
	KMSConfigMapName = "ocs-kms-connection-details"
	// KMSTokenSecretName is the name of the secret which has KMS token details
	KMSTokenSecretName = "ocs-kms-token"
	// KMSProviderKey is the key in config map to get the KMS provider name
	KMSProviderKey = "KMS_PROVIDER"
	// VaultKMSProvider a constant to represent 'vault' KMS provider
	VaultKMSProvider = "vault"
)

var (
	// currently supported KMS providers mapped to their address key
	kmsProviderAddressKeyMap = map[string]string{
		VaultKMSProvider: "VAULT_ADDR",
	}
)

// kmsConfigMapValidateFunc is a functional type,
// which validates the given configmap and returns an error if any
type kmsConfigMapValidateFunc func(*corev1.ConfigMap) error

var (
	// just to make sure 'reachKMSProvider' function follows the validate function type
	_ kmsConfigMapValidateFunc = reachKMSProvider
)

// getKMSConfigMap function try to return a KMS ConfigMap.
// if 'kmsValidateFunc' function is present it try to validate the retrieved config map.
func getKMSConfigMap(instance *ocsv1.StorageCluster, client client.Client, kmsValidateFunc kmsConfigMapValidateFunc) (*corev1.ConfigMap, error) {
	// if 'KMS' is not enabled, nothing to fetch
	if !instance.Spec.Encryption.KeyManagementService.Enable {
		return nil, nil
	}
	kmsConfigMap := corev1.ConfigMap{}
	err := client.Get(context.TODO(),
		types.NamespacedName{
			Name:      KMSConfigMapName,
			Namespace: instance.ObjectMeta.Namespace,
		},
		&kmsConfigMap,
	)
	if err != nil {
		return nil, err
	}
	// if validation function is provided, use it
	if kmsValidateFunc != nil {
		err = kmsValidateFunc(&kmsConfigMap)
	}
	return &kmsConfigMap, err
}

// reachKMSProvider function checks whether the provided address is reachable or not.
// This function won't validate any other cases and only returns an error if the provided
// KMS provider address is not reachable. All other validations will be done on rook side.
func reachKMSProvider(kmsConfigMap *corev1.ConfigMap) error {
	if kmsConfigMap == nil {
		return fmt.Errorf("please provide a valid config map")
	}
	kmsProviderName, ok := kmsConfigMap.Data[KMSProviderKey]
	if !ok {
		return nil
	}
	var kmsProviderAddressKey string
	if kmsProviderAddressKey, ok = kmsProviderAddressKeyMap[kmsProviderName]; !ok {
		// cannot find an address key specific for this KMS provider
		// nothing to validate
		return nil
	}
	kmsAddress, ok := kmsConfigMap.Data[kmsProviderAddressKey]
	if !ok {
		return nil
	}
	return checkEndpointReachable(kmsAddress, 5*time.Second)
}
