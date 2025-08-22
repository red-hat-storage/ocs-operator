package server

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

	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/services"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"testing"
)

const (
	testNamespace = "test-namespace"
)

// testKeyPair holds RSA key pair for testing
type testKeyPair struct {
	privateKey *rsa.PrivateKey
	publicKey  *rsa.PublicKey
	publicPEM  []byte
}

// generateTestKeyPair creates a new RSA key pair for testing
func generateTestKeyPair(t *testing.T) *testKeyPair {
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	assert.NoError(t, err)

	publicKey := &privateKey.PublicKey
	pubKeyBytes := x509.MarshalPKCS1PublicKey(publicKey)
	pubKeyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PUBLIC KEY",
		Bytes: pubKeyBytes,
	})

	return &testKeyPair{
		privateKey: privateKey,
		publicKey:  publicKey,
		publicPEM:  pubKeyPEM,
	}
}

func createOnboardingSecret(t *testing.T, fakeClient client.Client, consumerUID types.UID, ticket string) {
	onboardingSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("onboarding-token-%s", consumerUID),
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			defaults.OnboardingTokenKey: []byte(ticket),
		},
	}
	err := fakeClient.Create(context.Background(), onboardingSecret)
	assert.NoError(t, err)
}

// createOnboardingKeySecret creates the onboarding-ticket-key secret
func createOnboardingKeySecret(t *testing.T, fakeClient client.Client, publicPEM []byte) {
	keySecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "onboarding-ticket-key",
			Namespace: testNamespace,
		},
		Data: map[string][]byte{
			"key": publicPEM,
		},
	}
	err := fakeClient.Create(context.Background(), keySecret)
	assert.NoError(t, err)
}

// signOnboardingTicket creates and signs an onboarding ticket
func signOnboardingTicket(t *testing.T, privateKey *rsa.PrivateKey, ticket services.OnboardingTicket) string {
	// Marshal ticket to JSON
	ticketJSON, err := json.Marshal(ticket)
	assert.NoError(t, err)

	// Sign the ticket
	hash := sha256.Sum256(ticketJSON)
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, hash[:])
	assert.NoError(t, err)

	// Create base64 encoded ticket
	ticketB64 := base64.StdEncoding.EncodeToString(ticketJSON)
	signatureB64 := base64.StdEncoding.EncodeToString(signature)

	return ticketB64 + "." + signatureB64
}
