package util

import (
	"cmp"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DefaultProviderServerSAN is the SAN of the provider server's service-CA-signed serving certificate.
const DefaultProviderServerSAN = "ocs-provider-server.openshift-storage.svc"

// SelfSignedCertValidity is the lifetime of certificates produced by GetSelfSignedCert.
const SelfSignedCertValidity = 47 * 24 * time.Hour

// GetSelfSignedCert generates a self-signed CA certificate and private key.
func GetSelfSignedCert(cn string, keyUsage x509.KeyUsage, extKeyUsage []x509.ExtKeyUsage) (keyPEM, certPEM []byte, err error) {
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to generate private key: %v", err)
	}

	template := &x509.Certificate{
		Subject: pkix.Name{
			CommonName: cn,
		},
		DNSNames:              []string{cn},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(SelfSignedCertValidity),
		KeyUsage:              keyUsage,
		ExtKeyUsage:           extKeyUsage,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &priv.PublicKey, priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create certificate: %v", err)
	}
	certPEM = pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	privBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to marshal private key: %v", err)
	}
	keyPEM = pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})

	return keyPEM, certPEM, nil
}

// LoadPeerTLSConfig loads the mTLS certificates configured on a StorageClusterPeer and returns a *tls.Config ready for the provider client.
func LoadPeerTLSConfig(
	ctx context.Context,
	k8sClient client.Client,
	peer *ocsv1.StorageClusterPeer,
) (*tls.Config, error) {
	serverCA, clientCert, clientKey, err := loadPeerCertificates(ctx, k8sClient, peer)
	if err != nil {
		return nil, err
	}

	// TODO: mandate mTLS by removing the both-or-neither guard and the insecure fallback below.
	if (len(clientCert) == 0) != (len(clientKey) == 0) {
		return nil, fmt.Errorf("client certificate and key must both be provided or both be empty")
	}

	var tlsConfig *tls.Config
	if len(serverCA) == 0 {
		// No server CA configured: skip server certificate verification (backward compatibility).
		tlsConfig = &tls.Config{InsecureSkipVerify: true}
	} else {
		certPool := x509.NewCertPool()
		if !certPool.AppendCertsFromPEM(serverCA) {
			return nil, fmt.Errorf("failed to parse server CA certificate")
		}
		tlsConfig = &tls.Config{
			RootCAs:    certPool,
			ServerName: cmp.Or(peer.Spec.ServerSAN, DefaultProviderServerSAN),
		}
	}

	// Attach the client certificate for mTLS, regardless of server verification mode.
	if len(clientCert) > 0 {
		cert, err := tls.X509KeyPair(clientCert, clientKey)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %v", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	return tlsConfig, nil
}

// loadPeerCertificates loads mTLS certificates for a StorageClusterPeer.
func loadPeerCertificates(
	ctx context.Context,
	k8sClient client.Client,
	peer *ocsv1.StorageClusterPeer,
) (serverCA, clientCert, clientKey []byte, err error) {
	if peer.Spec.ServerCAConfigMap.Name != "" {
		key := peer.Spec.ServerCAConfigMap.Key
		configMap := &corev1.ConfigMap{}
		configMap.Name = peer.Spec.ServerCAConfigMap.Name
		configMap.Namespace = peer.Namespace
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get server CA configmap %s: %w", configMap.Name, err)
		}
		if len(configMap.Data[key]) == 0 {
			return nil, nil, nil, fmt.Errorf("%s not found in server CA configmap %s", key, configMap.Name)
		}
		serverCA = []byte(configMap.Data[key])
	}

	if peer.Spec.ClientCertSecret.Name != "" {
		secret := &corev1.Secret{}
		secret.Name = peer.Spec.ClientCertSecret.Name
		secret.Namespace = peer.Namespace
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("failed to get client cert secret %s: %w", secret.Name, err)
		}
		clientCert = secret.Data["tls.crt"]
		clientKey = secret.Data["tls.key"]
		if len(clientCert) == 0 || len(clientKey) == 0 {
			return nil, nil, nil, fmt.Errorf("tls.crt or tls.key not found in client cert secret %s", secret.Name)
		}
	}

	// When the spec configures neither CA nor client cert, all returns are nil;
	// the caller decides how to proceed (e.g. fall back to an insecure connection).
	return serverCA, clientCert, clientKey, nil
}
