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

func testScheme(t *testing.T) *runtime.Scheme {
	t.Helper()
	s := runtime.NewScheme()
	require.NoError(t, corev1.AddToScheme(s))
	require.NoError(t, ocsv1.AddToScheme(s))
	return s
}

func TestLoadPeerTLSConfig(t *testing.T) {
	caPEM, _ := testCertKeyPEM(t)
	certPEM, keyPEM := testCertKeyPEM(t)

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
				assert.False(t, cfg.InsecureSkipVerify)
				assert.NotNil(t, cfg.RootCAs)
				assert.Equal(t, defaultProviderServerSAN, cfg.ServerName)
			},
		},
		{
			name: "CA set, explicit SAN overrides default",
			objs: []client.Object{caConfigMap("ca.crt", caPEM)},
			peer: peerWith(ocsv1.StorageClusterPeerSpec{ServerCAConfigMap: caRef, ServerSAN: "custom.san.svc"}),
			check: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, "custom.san.svc", cfg.ServerName)
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
				assert.NotNil(t, cfg.RootCAs)
				assert.Len(t, cfg.Certificates, 1)
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
