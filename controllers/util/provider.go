package util

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"

	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	// Name of existing private key which is used ocs-operator
	onboardingValidationPrivateKeySecretName = "onboarding-private-key"
)

// GenerateOnboardingToken generates a token valid for a duration of "tokenLifetimeInHours".
// The storageQuotaInGiB is optional, and it is used to limit the storage of PVC in the application cluster.
func GenerateOnboardingToken(tokenLifetimeInHours int, cl client.Client, storageQuotaInGiB *uint) (string, error) {
	tokenExpirationDate := time.Now().
		Add(time.Duration(tokenLifetimeInHours) * time.Hour).
		Unix()

	ticket := services.OnboardingTicket{
		ID:             uuid.New().String(),
		ExpirationDate: tokenExpirationDate,
	}
	if storageQuotaInGiB != nil {
		ticket.StorageQuotaInGiB = *storageQuotaInGiB
	}
	payload, err := json.Marshal(ticket)
	if err != nil {
		return "", fmt.Errorf("failed to marshal the payload: %v", err)
	}

	encodedPayload := base64.StdEncoding.EncodeToString(payload)
	// Before signing, we need to hash our message
	// The hash is what we actually sign
	msgHash := sha256.New()
	_, err = msgHash.Write(payload)
	if err != nil {
		return "", fmt.Errorf("failed to hash onboarding token payload: %v", err)
	}

	privateKey, err := decodePemKey(cl)
	if err != nil {
		return "", fmt.Errorf("failed to decode the private key: %v", err)
	}

	msgHashSum := msgHash.Sum(nil)
	// In order to generate the signature, we provide a random number generator,
	// our private key, the hashing algorithm that we used, and the hash sum
	// of our message
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, msgHashSum)
	if err != nil {
		return "", fmt.Errorf("failed to sign private key: %v", err)
	}

	encodedSignature := base64.StdEncoding.EncodeToString(signature)
	return fmt.Sprintf("%s.%s", encodedPayload, encodedSignature), nil
}

func decodePemKey(cl client.Client) (*rsa.PrivateKey, error) {
	klog.Info("Decoding the Pem key")
	ctx := context.Background()
	operatorNamespace, err := GetOperatorNamespace()
	if err != nil {
		return nil, fmt.Errorf("unable to get operator namespace: %v", err)
	}

	privateSecret := &corev1.Secret{}
	privateSecret.Name = onboardingValidationPrivateKeySecretName
	privateSecret.Namespace = operatorNamespace

	err = cl.Get(ctx, types.NamespacedName{Name: onboardingValidationPrivateKeySecretName, Namespace: operatorNamespace}, privateSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get private secret: %v", err)
	}

	Block, _ := pem.Decode(privateSecret.Data["key"])
	privateKey, err := x509.ParsePKCS1PrivateKey(Block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}

	return privateKey, nil
}
