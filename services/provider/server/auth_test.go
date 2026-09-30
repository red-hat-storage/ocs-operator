package server

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"testing"
	"time"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

// generateTestCA generates a test CA certificate and private key
func generateTestCA() (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test CA"},
			CommonName:   "Test CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, nil, nil, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	return cert, priv, certPEM, nil
}

// generateTestClientCert generates a test client certificate signed by the CA
func generateTestClientCert(caCert *x509.Certificate, caKey *ecdsa.PrivateKey, san string) (*x509.Certificate, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Test Client"},
			CommonName:   "Test Client",
		},
		DNSNames:              []string{san},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, caCert, &priv.PublicKey, caKey)
	if err != nil {
		return nil, err
	}

	cert, err := x509.ParseCertificate(certDER)
	if err != nil {
		return nil, err
	}

	return cert, nil
}

// TestAuthenticateClientConnection tests StorageConsumer client certificate authentication
func TestAuthenticateClientConnection(t *testing.T) {
	// Generate test CA and client cert
	caCert, caKey, caPEM, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate test CA: %v", err)
	}

	expectedSAN := "test-client.example.com"
	clientCert, err := generateTestClientCert(caCert, caKey, expectedSAN)
	if err != nil {
		t.Fatalf("Failed to generate test client cert: %v", err)
	}

	tests := []struct {
		name            string
		consumer        *ocsv1alpha1.StorageConsumer
		caSecret        *corev1.Secret
		peerCerts       []*x509.Certificate
		expectedError   bool
		skipPeerContext bool
	}{
		{
			name: "secrets not configured - should skip authentication",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					// No ClientCASecret or ClientSAN configured
				},
			},
			expectedError: false,
		},
		{
			name: "disable annotation set - should skip authentication",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: "test-namespace",
					Annotations: map[string]string{
						DisableClientCertValidation: "true",
					},
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCASecret: corev1.LocalObjectReference{Name: "test-ca-secret"},
					ClientSAN:      expectedSAN,
				},
			},
			expectedError: false,
		},
		{
			name: "valid client certificate - should succeed",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCASecret: corev1.LocalObjectReference{Name: "test-ca-secret"},
					ClientSAN:      expectedSAN,
				},
			},
			caSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca-secret",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					"ca.crt": caPEM,
				},
			},
			peerCerts:     []*x509.Certificate{clientCert},
			expectedError: false,
		},
		{
			name: "no client certificate provided - should fail",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCASecret: corev1.LocalObjectReference{Name: "test-ca-secret"},
					ClientSAN:      expectedSAN,
				},
			},
			caSecret: &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-ca-secret",
					Namespace: "test-namespace",
				},
				Data: map[string][]byte{
					"ca.crt": caPEM,
				},
			},
			peerCerts:     nil,
			expectedError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = ocsv1alpha1.AddToScheme(scheme)

			var objects []client.Object
			if tt.caSecret != nil {
				objects = append(objects, tt.caSecret)
			}

			fakeClient := fake.NewClientBuilder().
				WithScheme(scheme).
				WithObjects(objects...).
				Build()

			server := &OCSProviderServer{
				client:    fakeClient,
				namespace: "test-namespace",
			}

			ctx := context.Background()
			if !tt.skipPeerContext {
				p := &peer.Peer{
					AuthInfo: credentials.TLSInfo{
						State: tls.ConnectionState{
							PeerCertificates: tt.peerCerts,
						},
					},
				}
				ctx = peer.NewContext(ctx, p)
			}

			err := server.authenticateClientConnection(ctx, tt.consumer)

			if tt.expectedError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectedError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}
		})
	}
}
