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

	"github.com/google/uuid"
	"github.com/red-hat-storage/ocs-operator/v4/services"
)

// GenerateOnboardingToken generates a token valid for a duration of "tokenLifetimeInHours".
// The token content is predefined and signed by the private key which'll be read from supplied "privateKeyPath".
// The roleSpec contains the SubjectRole and the RoleOptions for the ticket.
func GenerateOnboardingToken(tokenLifetimeInHours int, privateKeyPath string, roleSpec services.OnboardingSubjectRoleSpec) (string, error) {
	tokenExpirationDate := time.Now().
		Add(time.Duration(tokenLifetimeInHours) * time.Hour).
		Unix()

	ticket := services.OnboardingTicket{
		ID:             uuid.New().String(),
		ExpirationDate: tokenExpirationDate,
		SubjectRole:    roleSpec,
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
