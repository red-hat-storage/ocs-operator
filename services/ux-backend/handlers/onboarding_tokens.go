package handler

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
	"net/http"
	"os"
	"time"

	"github.com/google/uuid"
	"github.com/red-hat-storage/ocs-operator/v4/services/types"
	"k8s.io/klog/v2"
)

const onboardingPrivateKeyFilePath = "/etc/private-key/key"

func OnboardingTokensHandler(w http.ResponseWriter, r *http.Request, tokenLifetimeInHours int) {

	var err error
	switch r.Method {
	case "POST":

		onboardingToken, err := generateOnboardingToken(tokenLifetimeInHours)
		if err != nil {
			klog.Errorf("failed to get onboardig token: %v", err)
			w.WriteHeader(http.StatusInternalServerError)
			w.Header().Set("Content-Type", "text/text")
			_, err = w.Write([]byte("Failed to generate token"))

			if err != nil {
				klog.Errorf("failed write data to response writer, %v", err)
			}
			return
		}

		klog.Info("onboarding token generated successfully")
		w.WriteHeader(http.StatusOK)
		w.Header().Set("Content-Type", "text/text")

		_, err = w.Write([]byte(onboardingToken))
		if err != nil {
			klog.Errorf("failed write data to response writer: %v", err)
			return
		}

	default:
		klog.Info("Only POST method should be used to send data to this endpoint /onboarding-tokens")
		w.WriteHeader(http.StatusMethodNotAllowed)
		w.Header().Set("Content-Type", "text/text")
		_, err = w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method)))
		if err != nil {
			klog.Errorf("failed write data to response writer: %v", err)
		}
		return
	}
}

func generateOnboardingToken(tokenLifetimeInHours int) (string, error) {

	tokenExpirationDate := time.Now().
		Add(time.Duration(tokenLifetimeInHours) * time.Hour).
		Unix()

	payload, err := json.Marshal(types.OnboardingTicket{
		ID:             uuid.New().String(),
		ExpirationDate: tokenExpirationDate,
	})

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

	privateKey, err := readAndDecodeOnboardingPrivateKey()
	if err != nil {
		return "", fmt.Errorf("failed to read and decode private key: %v", err)
	}

	msgHashSum := msgHash.Sum(nil)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, msgHashSum)
	if err != nil {
		return "", fmt.Errorf("failed to sign private key: %v", err)
	}

	encodedSignature := base64.StdEncoding.EncodeToString(signature)
	return fmt.Sprintf("%s.%s", encodedPayload, encodedSignature), nil
}

func readAndDecodeOnboardingPrivateKey() (*rsa.PrivateKey, error) {

	pemString, err := os.ReadFile(onboardingPrivateKeyFilePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read onboarding private key: %v", err)
	}

	// In order to generate the signature, we provide a random number generator,
	// our private key, the hashing algorithm that we used, and the hash sum
	// of our message
	Block, _ := pem.Decode(pemString)
	privateKey, err := x509.ParsePKCS1PrivateKey(Block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse private key: %v", err)
	}
	return privateKey, nil
}
