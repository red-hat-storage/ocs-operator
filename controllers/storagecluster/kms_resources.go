package storagecluster

import (
	"context"
	"fmt"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// KMSConfigMapName is the name configmap which has KMS config details
	KMSConfigMapName = "ocs-kms-connection-details"
	// CSIKMSConfigMapName is the name of configmap provided to the CSI Operator
	CSIKMSConfigMapName = "csi-kms-connection-details"
	// KMSTokenSecretName is the name of the secret which has KMS token details
	KMSTokenSecretName = "ocs-kms-token"
	// CSIKMSTokenSecretName is the name of the secret which has the KMS token details of encrypted storage class
	CSIKMSTokenSecretName = "ceph-csi-kms-token"
	// KMSProviderKey is the key in config map to get the KMS provider name
	KMSProviderKey = "KMS_PROVIDER"
	// VaultKMSProvider a constant to represent 'vault' KMS provider
	VaultKMSProvider = "vault"
	// VaultTokenAuthMethod is the key to represent vault token based authentication
	VaultTokenAuthMethod = "token"
	// VaultSAAuthMethod is the key to represent vault k8s service account based authentication
	VaultSAAuthMethod = "kubernetes"
	// IbmKeyProtectKMSProvider a constant to represent IBM hpcs KMS provider
	IbmKeyProtectKMSProvider = "ibmkeyprotect"
	// ThalesKMSProvider a constant to represent Thales (using KMIP) KMS provider
	ThalesKMSProvider = "kmip"
	// AzureKSMProvider represents the Azure Key vault.
	AzureKSMProvider = "azure-kv"
)

var (
	// currently supported KMS providers mapped to their address key
	kmsProviderAddressKeyMap = map[string]string{
		VaultKMSProvider:  "VAULT_ADDR",
		AzureKSMProvider:  "AZURE_VAULT_URL",
		ThalesKMSProvider: "KMIP_ENDPOINT",
	}
	// Mapping of KMS providers and key where corresponding Secret name is stored
	kmsProviderSecretKeyMap = map[string]string{
		IbmKeyProtectKMSProvider: "IBM_KP_SECRET_NAME",
		ThalesKMSProvider:        "KMIP_SECRET_NAME",
	}
)

func deleteKMSResources(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	// if 'KMS' is not enabled, nothing to delete
	if !sc.Spec.Encryption.KeyManagementService.Enable {
		return nil
	}

	type getKMSResourceFunc func(*ocsv1.StorageCluster, client.Client) (client.Object, error)

	getKMSConfigMapAsRuntimeObject := func(
		sc *ocsv1.StorageCluster,
		client client.Client) (client.Object, error) {
		return getKMSConfigMap(KMSConfigMapName, sc, client)
	}

	getCSIKMSConfigMapAsRuntimeObject := func(
		sc *ocsv1.StorageCluster,
		client client.Client) (client.Object, error) {
		return getKMSConfigMap(CSIKMSConfigMapName, sc, client)
	}

	getKMSSecretTokenAsRuntimeObject := func(
		sc *ocsv1.StorageCluster,
		client client.Client) (client.Object, error) {
		return getKMSSecretToken(sc, client)
	}

	getCSIKMSSecretTokenAsRuntimeObject := func(
		sc *ocsv1.StorageCluster,
		client client.Client) (client.Object, error) {
		return getCSIKMSSecretToken(sc, client)
	}

	resourceNameGetFuncMap := map[string]getKMSResourceFunc{
		KMSConfigMapName:      getKMSConfigMapAsRuntimeObject,
		CSIKMSConfigMapName:   getCSIKMSConfigMapAsRuntimeObject,
		KMSTokenSecretName:    getKMSSecretTokenAsRuntimeObject,
		CSIKMSTokenSecretName: getCSIKMSSecretTokenAsRuntimeObject,
	}
	// collect all the errors into a single return error
	var returnError error

	// TODO:
	// later this loop could be refactored to use `go-routines` & `channels`,
	// but it needs broader discussion to bring uniformity in other deletion/uninstall modules
	for kmsResourceName, kmsResourceGetFunc := range resourceNameGetFuncMap {
		errLog := "while retrieving"
		runtimeObj, err := kmsResourceGetFunc(sc, r.Client)
		if err == nil {
			errLog = "while deleting"
			err = r.Client.Delete(context.TODO(), runtimeObj)
		}
		// ignore any NotFound error,
		// that is; resource is not there, nothing has to be deleted
		if errors.IsNotFound(err) {
			continue
		}
		if err != nil {
			if errLog == "while retrieving" {
				r.Log.Error(err, "Uninstall: Error occurred while retrieving the KMS resource.", "KMSResource", klog.KRef(sc.Namespace, kmsResourceName))
			} else {
				r.Log.Error(err, "Uninstall: Error occurred while deleting the KMS resource.", "KMSResource", klog.KRef(sc.Namespace, kmsResourceName))
			}
		}
		// collect the error into the return error
		if err != nil {
			formattedErr := fmt.Errorf("KMS Error: %v", err)
			if returnError == nil {
				returnError = formattedErr
			} else {
				returnError = fmt.Errorf("%v\n%v", returnError, formattedErr)
			}
		}
	}

	if returnError == nil {
		r.Log.Info("Uninstall: All KMS resources removed successfully.")
	}

	return returnError
}

// getKMSConfigMap function try to return a KMS ConfigMap.
// if 'kmsValidateFunc' function is present it try to validate the retrieved config map.
func getKMSConfigMap(configMapName string, instance *ocsv1.StorageCluster, client client.Client) (*corev1.ConfigMap, error) {
	// if 'KMS' is not enabled, nothing to fetch
	if !instance.Spec.Encryption.KeyManagementService.Enable {
		return nil, nil
	}
	if configMapName == "" {
		configMapName = KMSConfigMapName
	}
	kmsConfigMap := corev1.ConfigMap{}
	err := client.Get(context.TODO(),
		types.NamespacedName{
			Name:      configMapName,
			Namespace: instance.ObjectMeta.Namespace,
		},
		&kmsConfigMap,
	)
	if err != nil {
		return nil, err
	}
	return &kmsConfigMap, err
}

// getKMSSecretToken function try to return the KMS Secret Token
func getKMSSecretToken(instance *ocsv1.StorageCluster, client client.Client) (*corev1.Secret, error) {
	// if 'KMS' is not enabled, nothing to fetch
	if !instance.Spec.Encryption.KeyManagementService.Enable {
		return nil, nil
	}
	kmsSecretToken := &corev1.Secret{}
	err := client.Get(context.TODO(),
		types.NamespacedName{
			Name:      KMSTokenSecretName,
			Namespace: instance.ObjectMeta.Namespace,
		},
		kmsSecretToken,
	)
	return kmsSecretToken, err
}

// getCSIKMSSecretToken function try to return the KMS Secret Token of encrypted storageClass
func getCSIKMSSecretToken(instance *ocsv1.StorageCluster, client client.Client) (*corev1.Secret, error) {
	// if 'KMS' is not enabled, nothing to fetch
	if !instance.Spec.Encryption.KeyManagementService.Enable {
		return nil, nil
	}
	csiKMSSecretToken := &corev1.Secret{}
	err := client.Get(context.TODO(),
		types.NamespacedName{
			Name:      CSIKMSTokenSecretName,
			Namespace: instance.ObjectMeta.Namespace,
		},
		csiKMSSecretToken,
	)
	return csiKMSSecretToken, err
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
