package util

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

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

const testNamespace = "openshift-storage"

// testCertKeyPEM generates a self-signed certificate and its private key, PEM-encoded.
func testCertKeyPEM(t *testing.T) (certPEM, keyPEM []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	tmpl := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	require.NoError(t, err)
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, err := x509.MarshalECPrivateKey(key)
	require.NoError(t, err)
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return certPEM, keyPEM
}

func testServerCert(t *testing.T, caPEM, caKeyPEM []byte, dnsName string) *x509.Certificate {
	return testServerCertChain(t, caPEM, caKeyPEM, dnsName, false)[0]
}

func testServerCertChain(t *testing.T, caPEM, caKeyPEM []byte, dnsName string, withIntermediate bool) []*x509.Certificate {
	t.Helper()

	caBlock, _ := pem.Decode(caPEM)
	require.NotNil(t, caBlock)
	caCert, err := x509.ParseCertificate(caBlock.Bytes)
	require.NoError(t, err)

	caKeyBlock, _ := pem.Decode(caKeyPEM)
	require.NotNil(t, caKeyBlock)
	caKey, err := x509.ParseECPrivateKey(caKeyBlock.Bytes)
	require.NoError(t, err)

	signingCert := caCert
	signingKey := caKey
	var chain []*x509.Certificate
	if withIntermediate {
		intermediateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		require.NoError(t, err)
		intermediateTemplate := x509.Certificate{
			SerialNumber:          big.NewInt(3),
			Subject:               pkix.Name{CommonName: "intermediate"},
			NotBefore:             time.Now().Add(-time.Hour),
			NotAfter:              time.Now().Add(time.Hour),
			KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
			BasicConstraintsValid: true,
			IsCA:                  true,
		}
		intermediateDER, err := x509.CreateCertificate(rand.Reader, &intermediateTemplate, caCert, &intermediateKey.PublicKey, caKey)
		require.NoError(t, err)
		intermediateCert, err := x509.ParseCertificate(intermediateDER)
		require.NoError(t, err)
		signingCert = intermediateCert
		signingKey = intermediateKey
		chain = append(chain, intermediateCert)
	}

	serverKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)
	template := x509.Certificate{
		SerialNumber:          big.NewInt(2),
		Subject:               pkix.Name{CommonName: dnsName},
		DNSNames:              []string{dnsName},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, signingCert, &serverKey.PublicKey, signingKey)
	require.NoError(t, err)
	serverCert, err := x509.ParseCertificate(der)
	require.NoError(t, err)
	return append([]*x509.Certificate{serverCert}, chain...)
}

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, ocsv1.AddToScheme(s))
	return s
}

func TestLoadPeerTLSConfig(t *testing.T) {
	caPEM, caKeyPEM := testCertKeyPEM(t)
	certPEM, keyPEM := testCertKeyPEM(t)
	defaultSAN := DefaultProviderServerSAN(defaults.OcsProviderServerName, testNamespace)
	defaultServerCert := testServerCert(t, caPEM, caKeyPEM, defaultSAN)
	customServerCert := testServerCert(t, caPEM, caKeyPEM, "custom.san.svc")
	intermediateServerChain := testServerCertChain(t, caPEM, caKeyPEM, defaultSAN, true)

	caConfigMap := func(key string, data []byte) *corev1.ConfigMap {
		return &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: "ca-cm", Namespace: testNamespace},
			Data:       map[string]string{key: string(data)},
		}
	}
	clientSecret := func(data map[string][]byte) *corev1.Secret {
		return &corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{Name: "client-cert", Namespace: testNamespace},
			Data:       data,
		}
	}
	peerWith := func(spec ocsv1.StorageClusterPeerSpec) *ocsv1.StorageClusterPeer {
		return &ocsv1.StorageClusterPeer{
			ObjectMeta: metav1.ObjectMeta{Name: "peer", Namespace: testNamespace},
			Spec:       spec,
		}
	}
	caRef := corev1.ConfigMapKeySelector{
		LocalObjectReference: corev1.LocalObjectReference{Name: "ca-cm"},
		Key:                  "ca.crt",
	}

	tests := []struct {
		name    string
		objs    []client.Object
		peer    *ocsv1.StorageClusterPeer
		wantErr bool
		check   func(t *testing.T, cfg *tls.Config)
	}{
		{
			name: "no CA, no client cert -> insecure",
			peer: peerWith(ocsv1.StorageClusterPeerSpec{}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
				assert.Nil(t, cfg.RootCAs)
				assert.Empty(t, cfg.Certificates)
				assert.Empty(t, cfg.ServerName)
			},
		},
		{
			name: "CA set, default SAN",
			objs: []client.Object{caConfigMap("ca.crt", caPEM)},
			peer: peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
				assert.Nil(t, cfg.RootCAs)
				assert.Empty(t, cfg.ServerName)
				require.NotNil(t, cfg.VerifyConnection)
				assert.NoError(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{defaultServerCert}}))
				assert.Error(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{customServerCert}}))
			},
		},
		{
			name: "CA set, explicit SAN overrides default",
			objs: []client.Object{caConfigMap("ca.crt", caPEM)},
			peer: peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef, ServerSAN: "custom.san.svc"}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
				assert.Empty(t, cfg.ServerName)
				require.NotNil(t, cfg.VerifyConnection)
				assert.NoError(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{customServerCert}}))
				assert.Error(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: []*x509.Certificate{defaultServerCert}}))
			},
		},
		{
			name: "CA + client cert -> mTLS",
			objs: []client.Object{
				caConfigMap("ca.crt", caPEM),
				clientSecret(map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM}),
			},
			peer: peerWith(ocsv1.StorageClusterPeerSpec{
				ServerCAConfigMap: caRef,
				ClientCertSecret:  corev1.LocalObjectReference{Name: "client-cert"},
			}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
				require.NotNil(t, cfg.VerifyConnection)
				assert.Len(t, cfg.Certificates, 1)
				assert.NoError(t, cfg.VerifyConnection(tls.ConnectionState{PeerCertificates: intermediateServerChain}))
			},
		},
		{
			name: "client cert without CA -> insecure but still presents cert",
			objs: []client.Object{clientSecret(map[string][]byte{"tls.crt": certPEM, "tls.key": keyPEM})},
			peer: peerWith(ocsv1.StorageClusterPeerSpec{
				ClientCertSecret: corev1.LocalObjectReference{Name: "client-cert"},
			}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
				assert.Len(t, cfg.Certificates, 1)
			},
		},
		{
			name:    "malformed CA PEM",
			objs:    []client.Object{caConfigMap("ca.crt", []byte("not a pem"))},
			peer:    peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef}),
			wantErr: true,
		},
		{
			name:    "CA configmap missing",
			peer:    peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef}),
			wantErr: true,
		},
		{
			name:    "CA configmap key absent",
			objs:    []client.Object{caConfigMap("other.crt", caPEM)},
			peer:    peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef}),
			wantErr: true,
		},
		{
			name:    "client secret missing",
			peer:    peerWith(ocsv1.StorageClusterPeerSpec{ClientCertSecret: corev1.LocalObjectReference{Name: "client-cert"}}),
			wantErr: true,
		},
		{
			name:    "client secret missing tls.key",
			objs:    []client.Object{clientSecret(map[string][]byte{"tls.crt": certPEM})},
			peer:    peerWith(ocsv1.StorageClusterPeerSpec{ClientCertSecret: corev1.LocalObjectReference{Name: "client-cert"}}),
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k8sClient := fake.NewClientBuilder().WithScheme(testScheme(t)).WithObjects(tc.objs...).Build()
			cfg, err := LoadPeerTLSConfig(context.Background(), k8sClient, tc.peer)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.NotNil(t, cfg)
			tc.check(t, cfg)
		})
	}
}

func TestGetSelfSignedCert(t *testing.T) {
	cn := "ocs-internal-client"
	keyUsage := x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature
	extKeyUsage := []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}

	keyPEM, certPEM, err := GetSelfSignedCert(cn, keyUsage, extKeyUsage)
	assert.NoError(t, err)
	assert.NotEmpty(t, keyPEM)
	assert.NotEmpty(t, certPEM)

	block, _ := pem.Decode(certPEM)
	assert.NotNil(t, block)

	cert, err := x509.ParseCertificate(block.Bytes)
	assert.NoError(t, err)

	assert.Equal(t, cn, cert.Subject.CommonName)
	assert.Contains(t, cert.DNSNames, cn)
	assert.True(t, cert.IsCA)
	assert.True(t, cert.BasicConstraintsValid)
	assert.Equal(t, keyUsage, cert.KeyUsage)
	assert.Contains(t, cert.ExtKeyUsage, x509.ExtKeyUsageClientAuth)

	// Validity should be ~47 days
	validity := cert.NotAfter.Sub(cert.NotBefore)
	assert.InDelta(t, SelfSignedCertValidity.Hours(), validity.Hours(), 1)

	// The private key must decode
	keyBlock, _ := pem.Decode(keyPEM)
	assert.NotNil(t, keyBlock)

	// x509.CreateCertificate auto-generates a serial; each call yields a fresh, unique one
	assert.NotNil(t, cert.SerialNumber)
	_, certPEM2, err := GetSelfSignedCert(cn, keyUsage, extKeyUsage)
	assert.NoError(t, err)
	block2, _ := pem.Decode(certPEM2)
	cert2, err := x509.ParseCertificate(block2.Bytes)
	assert.NoError(t, err)
	assert.NotEqual(t, cert.SerialNumber, cert2.SerialNumber)
}

func TestCertNeedsRenewal(t *testing.T) {
	const renewalWindow = time.Hour

	tests := []struct {
		name   string
		expiry string
		want   bool
	}{
		{
			name:   "outside renewal window",
			expiry: time.Now().Add(2 * time.Hour).Format(time.RFC3339),
			want:   false,
		},
		{
			name:   "inside renewal window",
			expiry: time.Now().Add(30 * time.Minute).Format(time.RFC3339),
			want:   true,
		},
		{
			name:   "invalid expiry",
			expiry: "invalid",
			want:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			secret := &corev1.Secret{
				ObjectMeta: metav1.ObjectMeta{
					Annotations: map[string]string{defaults.CertExpiryAnnotation: tt.expiry},
				},
			}
			assert.Equal(t, tt.want, CertNeedsRenewal(secret, renewalWindow))
		})
	}
}
