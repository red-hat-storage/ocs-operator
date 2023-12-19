package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
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
	onboardingTicketPublicKeySecretName = "onboarding-ticket-key" //Name of existing public key which is used ocs-operator
	onboardingPrivateKeySecretName      = "onboarding-private-key"
	serviceAccountName                  = "onboarding-secret-generator"
)

func main() {
	clientset, err := newClient()
	if err != nil {
		klog.Error(err, "failed to create controller-runtime client")
		return
	}

	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		klog.Error(err, "unable to get operator namespace")
		os.Exit(1)
	}

	// 1. Check public key secret exist or not
	_, err = clientset.CoreV1().Secrets(operatorNamespace).Get(context.TODO(), onboardingTicketPublicKeySecretName, metav1.GetOptions{})

	if err != nil && kerrors.IsNotFound(err) {
		// Generate RSA key.
		var err error
		privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
		if err != nil {
			klog.Error(err, "unable to generate private")
			os.Exit(1)
		}

		publicKey := &privateKey.PublicKey
		// Export the keys to pem string
		privatePem := convertRsaPrivateKeyAsPemStr(privateKey)
		publicPem, err := convertRsaPublicKeyAsPemStr(publicKey)

		if err != nil {
			klog.Error(err, "failed to convert public key to pem str")
			os.Exit(1)
		}

		privateSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:        onboardingPrivateKeySecretName,
				Namespace:   operatorNamespace,
				Annotations: map[string]string{"kubernetes.io/service-account.name": serviceAccountName},
			},
			Type: "kubernetes.io/service-account-token",
			StringData: map[string]string{
				"key": privatePem,
			},
		}

		_, err = clientset.CoreV1().Secrets(operatorNamespace).Create(context.Background(), privateSecret, metav1.CreateOptions{})

		if err != nil {
			klog.Error(err, "Failed to create private secret.")
			os.Exit(1)
		}
		publicSecret := &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      onboardingTicketPublicKeySecretName,
				Namespace: operatorNamespace,
			},
			StringData: map[string]string{
				"key": publicPem,
			},
		}

		_, err = clientset.CoreV1().Secrets(operatorNamespace).Create(context.Background(), publicSecret, metav1.CreateOptions{})
		if err != nil {
			klog.Error(err, "Failed to create public secret.")
			os.Exit(1)
		}

	}

}

func newClient() (*kubernetes.Clientset, error) {
	config := runtime.GetConfigOrDie()
	clientset, err := kubernetes.NewForConfig(config)

	if err != nil {
		klog.Error(err, "failed to get clientset")
	}

	return clientset, nil
}

func convertRsaPrivateKeyAsPemStr(privateKey *rsa.PrivateKey) string {
	privteKeyBytes := x509.MarshalPKCS1PrivateKey(privateKey)
	privateKeyPem := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privteKeyBytes})
	return string(privateKeyPem)
}

func convertRsaPublicKeyAsPemStr(publicKey *rsa.PublicKey) (string, error) {
	publicKeyBytes, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", err
	}
	publicKeyPem := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyBytes})

	return string(publicKeyPem), nil
}
