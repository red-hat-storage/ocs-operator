package util

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"time"

	"github.com/red-hat-storage/ocs-operator/v4/services"

	"github.com/google/uuid"
	"k8s.io/apimachinery/pkg/types"
)

const (
	ProviderCertsMountPoint = "/mnt/cert"
)

// GenerateClientOnboardingToken generates a ocs-client token valid for a duration of "tokenLifetimeInHours".
// The token content is predefined and signed by the private key which'll be read from supplied "privateKeyPath".
// The storageQuotaInGiB is optional, and it is used to limit the storage of PVC in the application cluster.
func GenerateClientOnboardingToken(tokenLifetimeInHours int, privateKeyPath string, storageQuotainGib *uint, storageClusterUID types.UID) (string, error) {
	tokenExpirationDate := time.Now().
		Add(time.Duration(tokenLifetimeInHours) * time.Hour).
		Unix()

	ticket := services.OnboardingTicket{
		ID:                uuid.New().String(),
		ExpirationDate:    tokenExpirationDate,
		SubjectRole:       services.ClientRole,
		StorageQuotaInGiB: storageQuotainGib,
		StorageCluster:    storageClusterUID,
	}

	token, err := encodeAndSignOnboardingToken(privateKeyPath, ticket)
	if err != nil {
		return "", err
	}
	return token, nil
}

// GeneratePeerOnboardingToken generates a ocs-peer token valid for a duration of "tokenLifetimeInHours".
// The token content is predefined and signed by the private key which'll be read from supplied "privateKeyPath".
func GeneratePeerOnboardingToken(tokenLifetimeInHours int, privateKeyPath string, storageClusterUID types.UID) (string, error) {
	tokenExpirationDate := time.Now().
		Add(time.Duration(tokenLifetimeInHours) * time.Hour).
		Unix()

	ticket := services.OnboardingTicket{
		ID:             uuid.New().String(),
		ExpirationDate: tokenExpirationDate,
		SubjectRole:    services.PeerRole,
		StorageCluster: storageClusterUID,
	}
	token, err := encodeAndSignOnboardingToken(privateKeyPath, ticket)
	if err != nil {
		return "", err
	}
	return token, nil
}

// encodeAndSignOnboardingToken generates a token from the ticket.
// The token content is predefined and signed by the private key which'll be read from supplied "privateKeyPath".
func encodeAndSignOnboardingToken(privateKeyPath string, ticket services.OnboardingTicket) (string, error) {
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

	privateKey, err := readAndDecodePrivateKey(privateKeyPath)
	if err != nil {
		return "", fmt.Errorf("failed to read and decode private key: %v", err)
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

func readAndDecodePrivateKey(privateKeyPath string) (*rsa.PrivateKey, error) {
	pemString, err := os.ReadFile(privateKeyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read private key: %v", err)
	}

	Block, _ := pem.Decode(pemString)
	privateKey, err := x509.ParsePKCS1PrivateKey(Block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}
	return privateKey, nil
}
