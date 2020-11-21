package storagecluster

import (
	"context"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
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

func getKMSConfigMap(instance *ocsv1.StorageCluster, client client.Client) (*corev1.ConfigMap, error) {
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
	return &kmsConfigMap, err
}
