package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"golang.org/x/net/context"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// Name of existing public key which is used ocs-operator
	onboardingValidationPublicKeySecretName  = "onboarding-ticket-key"
	onboardingValidationPrivateKeySecretName = "onboarding-private-key"
)

func main() {

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add corev1 to scheme. %v", err)
	}
	if err := ocsv1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add ocsv1 to scheme. %v", err)
	}

	cl, err := util.NewK8sClient(scheme)
	if err != nil {
		klog.Exitf("failed to create kube client: %v", err)
	}

	ctx := context.Background()

	namespace := util.GetPodNamespace()

	// Generate RSA key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 4096)
	if err != nil {
		klog.Exitf("unable to generate private key: %v", err)
	}

	publicKey := &privateKey.PublicKey
	// Export the keys to pem string
	privatePem := convertRsaPrivateKeyAsPemStr(privateKey)
	publicPem := convertRsaPublicKeyAsPemStr(publicKey)

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, cl, namespace)
	if err != nil {
		klog.Exitf("failed to get storage cluster: %v", err)
	}

	// In situations where there is a risk of one secret being updated and potentially
	// failing to update another, it is recommended not to rely solely on clientset update mechanisms.
	// Instead, a safer approach is to delete both secrets and then recreate them simultaneously
	// to ensure consistency and accuracy of all secrets. By this way it will be easier to diagnose the
	// issues if one or two secrets do not exist instead of trying to understand if they match
	privateSecret := &corev1.Secret{}
	privateSecret.Name = onboardingValidationPrivateKeySecretName
	privateSecret.Namespace = namespace
	err = cl.Delete(ctx, privateSecret)
	if err != nil && !kerrors.IsNotFound(err) {
		klog.Exitf("failed to delete private secret: %v", err)
	}

	// Delete public key secret
	publicSecret := &corev1.Secret{}
	publicSecret.Name = onboardingValidationPublicKeySecretName
	publicSecret.Namespace = namespace
	err = cl.Delete(ctx, publicSecret)
	if err != nil && !kerrors.IsNotFound(err) {
		klog.Exitf("failed to delete public secret: %v", err)
	}

	err = controllerutil.SetOwnerReference(storageCluster, privateSecret, cl.Scheme())
	if err != nil {
		klog.Exitf("failed to set owner reference for private secret: %v", err)
	}

	privateSecret.StringData = map[string]string{
		"key": privatePem,
	}

	err = cl.Create(ctx, privateSecret, &client.CreateOptions{})
	if err != nil {
		klog.Exitf("failed to create private secret: %v", err)
	}

	err = controllerutil.SetOwnerReference(storageCluster, publicSecret, cl.Scheme())
	if err != nil {
		klog.Exitf("failed to set owner reference for public secret: %v", err)
	}
	publicSecret.StringData = map[string]string{
		"key": publicPem,
	}

	err = cl.Create(ctx, publicSecret, &client.CreateOptions{})
	if err != nil {
		klog.Exitf("failed to create public secret: %v", err)
	}

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
