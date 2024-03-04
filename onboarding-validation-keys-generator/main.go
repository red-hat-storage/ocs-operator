package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
	runtime "sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	// Name of existing public key which is used ocs-operator
	onboardingValidationPublicKeySecretName  = "onboarding-ticket-key"
	onboardingValidationPrivateKeySecretName = "onboarding-private-key"
	storageClusterName                       = "ocs-storagecluster"
)

func main() {
	clientset, err := newClient()
	if err != nil {
		klog.Errorf("failed to create clientset: %v", err)
		os.Exit(1)
	}

	ctx := context.Background()
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		klog.Errorf("unable to get operator namespace: %v", err)
		os.Exit(1)
	}

	// Generate RSA key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		klog.Errorf("unable to generate private: %v", err)
		os.Exit(1)
	}

	publicKey := &privateKey.PublicKey
	// Export the keys to pem string
	privatePem := convertRsaPrivateKeyAsPemStr(privateKey)
	publicPem := convertRsaPublicKeyAsPemStr(publicKey)

	// In situations where there is a risk of one secret being updated and potentially
	// failing to update another, it is recommended not to rely solely on clientset update mechanisms.
	// Instead, a safer approach is to delete both secrets and then recreate them simultaneously
	// to ensure consistency and accuracy of all secrets. By this way it will be easier to diagnose the
	// issues if one or two secrets do not exist instead of trying to understand if they match
	err = clientset.CoreV1().
		Secrets(operatorNamespace).
		Delete(ctx, onboardingValidationPrivateKeySecretName, metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		klog.Errorf("failed to delete private secret: %v", err)
		os.Exit(1)
	}

	// Delete public key secret
	err = clientset.CoreV1().
		Secrets(operatorNamespace).
		Delete(ctx, onboardingValidationPublicKeySecretName, metav1.DeleteOptions{})
	if err != nil && !kerrors.IsNotFound(err) {
		klog.Errorf("failed to delete public secret: %v", err)
		os.Exit(1)
	}

	storageClusterMetadata, err := getStorageClusterMetadata(ctx, operatorNamespace, clientset)
	if err != nil {
		klog.Errorf("failed to get storage cluster metadata: %v", err)
		os.Exit(1)
	}

	privateSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      onboardingValidationPrivateKeySecretName,
			Namespace: operatorNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID:        storageClusterMetadata.UID,
					APIVersion: storageClusterMetadata.APIVersion,
					Kind:       storageClusterMetadata.Kind,
					Name:       storageClusterMetadata.Name,
				},
			}},
		StringData: map[string]string{
			"key": privatePem,
		},
	}

	_, err = clientset.CoreV1().
		Secrets(operatorNamespace).
		Create(ctx, privateSecret, metav1.CreateOptions{})

	if err != nil {
		klog.Errorf("failed to create private secret: %v", err)
		os.Exit(1)
	}

	publicSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      onboardingValidationPublicKeySecretName,
			Namespace: operatorNamespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					UID:        storageClusterMetadata.UID,
					APIVersion: storageClusterMetadata.APIVersion,
					Kind:       storageClusterMetadata.Kind,
					Name:       storageClusterMetadata.Name,
				},
			}},
		StringData: map[string]string{
			"key": publicPem,
		},
	}

	_, err = clientset.CoreV1().
		Secrets(operatorNamespace).
		Create(ctx, publicSecret, metav1.CreateOptions{})
	if err != nil {
		klog.Errorf("failed to create public secret: %v", err)
		os.Exit(1)
	}

}

func newClient() (*kubernetes.Clientset, error) {
	config := runtime.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	return clientset, nil
}

func convertRsaPrivateKeyAsPemStr(privateKey *rsa.PrivateKey) string {
	privteKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privteKeyBytes})
	return string(privateKeyPem)
}

func convertRsaPublicKeyAsPemStr(publicKey *rsa.PublicKey) string {
	publicKeyBytes := x509.MarshalPKCS1PublicKey(publicKey)
	publicKeyPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes})
	return string(publicKeyPem)
}

func getStorageClusterMetadata(ctx context.Context, operatorNamespace string, clientset *kubernetes.Clientset) (*metav1.PartialObjectMetadata, error) {
	var storageClusterMetadata metav1.PartialObjectMetadata
	storageClusterGVKPath := fmt.Sprintf(
		"/apis/ocs.openshift.io/v1/namespaces/%s/storageclusters/%s",
		operatorNamespace,
		storageClusterName,
	)
	storageClusterMetadataJSON, err := clientset.RESTClient().Get().AbsPath(storageClusterGVKPath).Do(ctx).Raw()
	if err != nil {
		return nil, fmt.Errorf("failed to get storage cluster metadata: %v", err)
	}

	if err = json.Unmarshal(storageClusterMetadataJSON, &storageClusterMetadata); err != nil {
		return nil, fmt.Errorf("failed to parse storage cluster metadata response: %v", err)
	}

	return &storageClusterMetadata, nil
}
