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

// generateTestCA generates a test root CA certificate and private key
func generateTestCA() (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Test Root CA"},
			CommonName:   "Test Root CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
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

// generateTestIntermediateCA generates a test intermediate CA certificate signed by the root CA
func generateTestIntermediateCA(rootCert *x509.Certificate, rootKey *ecdsa.PrivateKey) (*x509.Certificate, *ecdsa.PrivateKey, []byte, error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(100),
		Subject: pkix.Name{
			Organization: []string{"Test Intermediate CA"},
			CommonName:   "Test Intermediate CA",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(24 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, rootCert, &priv.PublicKey, rootKey)
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
		IsCA:                  false,
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
	// Test 1: Simple case - root CA directly signs client cert (no intermediate)
	rootCA, rootKey, rootPEM, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate root CA: %v", err)
	}

	simpleSAN := "simple-client.example.com"
	simpleClientCert, err := generateTestClientCert(rootCA, rootKey, simpleSAN)
	if err != nil {
		t.Fatalf("Failed to generate simple client cert: %v", err)
	}

	// Test 2: Chain case - root CA → intermediate CA → client cert
	chainRootCA, chainRootKey, chainRootPEM, err := generateTestCA()
	if err != nil {
		t.Fatalf("Failed to generate chain root CA: %v", err)
	}

	intermCA, intermKey, intermPEM, err := generateTestIntermediateCA(chainRootCA, chainRootKey)
	if err != nil {
		t.Fatalf("Failed to generate intermediate CA: %v", err)
	}

	chainSAN := "chain-client.example.com"
	chainClientCert, err := generateTestClientCert(intermCA, intermKey, chainSAN)
	if err != nil {
		t.Fatalf("Failed to generate chain client cert: %v", err)
	}

	// Full chain bundle (root + intermediate)
	chainBundle := append(chainRootPEM, intermPEM...)

	tests := []struct {
		name            string
		consumer        *ocsv1alpha1.StorageConsumer
		caConfigMap     *corev1.ConfigMap
		peerCerts       []*x509.Certificate
		expectedError   bool
		skipPeerContext bool
	}{
		{
			name: "configmap not configured - should skip authentication",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "test-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					// No ClientCAConfigMap or ClientSAN configured
				},
			},
			expectedError: false,
		},
		{
			name: "simple client cert without intermediate - should succeed",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCAConfigMap: corev1.LocalObjectReference{Name: "simple-ca-configmap"},
					ClientSAN:         simpleSAN,
				},
			},
			caConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-ca-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"ca.crt": string(rootPEM),
				},
			},
			peerCerts:     []*x509.Certificate{simpleClientCert},
			expectedError: false,
		},
		{
			name: "no client certificate provided - should fail",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "no-cert-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCAConfigMap: corev1.LocalObjectReference{Name: "simple-ca-configmap"},
					ClientSAN:         simpleSAN,
				},
			},
			caConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "simple-ca-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"ca.crt": string(rootPEM),
				},
			},
			peerCerts:     nil,
			expectedError: true,
		},
		{
			name: "chain with intermediate in configmap - should succeed",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "chain-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCAConfigMap: corev1.LocalObjectReference{Name: "chain-ca-configmap"},
					ClientSAN:         chainSAN,
				},
			},
			caConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "chain-ca-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"ca.crt": string(chainBundle), // Full chain: root + intermediate
				},
			},
			peerCerts:     []*x509.Certificate{chainClientCert},
			expectedError: false,
		},
		{
			name: "client provides intermediate cert - should succeed",
			consumer: &ocsv1alpha1.StorageConsumer{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "client-intermediate-consumer",
					Namespace: "test-namespace",
				},
				Spec: ocsv1alpha1.StorageConsumerSpec{
					ClientCAConfigMap: corev1.LocalObjectReference{Name: "root-only-configmap"},
					ClientSAN:         chainSAN,
				},
			},
			caConfigMap: &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "root-only-configmap",
					Namespace: "test-namespace",
				},
				Data: map[string]string{
					"ca.crt": string(chainRootPEM), // Only root CA
				},
			},
			peerCerts:     []*x509.Certificate{chainClientCert, intermCA}, // Client provides intermediate in PeerCertificates[1]
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme := runtime.NewScheme()
			_ = corev1.AddToScheme(scheme)
			_ = ocsv1alpha1.AddToScheme(scheme)

			var objects []client.Object
			if tt.caConfigMap != nil {
				objects = append(objects, tt.caConfigMap)
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
