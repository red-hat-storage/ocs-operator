package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
)

const (
	// DisableClientCertValidation annotation disables client certificate validation for StorageConsumer
	DisableClientCertValidation = "ocs.openshift.io/disable-client-cert-validation"
	// DisablePeerCertValidation annotation disables client certificate validation for StorageClusterPeer
	DisablePeerCertValidation = "ocs.openshift.io/disable-peer-cert-validation"
)

// validateCACert checks that the CA certificate is valid and has CA properties
func validateCACert(caCert []byte) error {
	block, _ := pem.Decode(caCert)
	if block == nil {
		return errors.New("invalid PEM format in CA certificate")
	}

	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return fmt.Errorf("failed to parse CA certificate: %w", err)
	}

	if !cert.IsCA {
		return fmt.Errorf("certificate is not a CA certificate (subject: %s)", cert.Subject.String())
	}

	now := time.Now()
	if now.Before(cert.NotBefore) || now.After(cert.NotAfter) {
		return fmt.Errorf("CA certificate expired or not yet valid (valid: %v to %v, subject: %s)",
			cert.NotBefore, cert.NotAfter, cert.Subject.String())
	}

	return nil
}

// validateClientCert validates a client certificate against a CA and expected SAN
func validateClientCert(caCert []byte, clientCert *x509.Certificate, expectedSAN string) error {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return errors.New("failed to parse CA certificate for verification")
	}

	opts := x509.VerifyOptions{
		Roots:     caCertPool,
		KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := clientCert.Verify(opts); err != nil {
		return fmt.Errorf("certificate verification failed (subject: %s, issuer: %s): %w",
			clientCert.Subject.String(), clientCert.Issuer.String(), err)
	}

	sanFound := false
	for _, san := range clientCert.DNSNames {
		if san == expectedSAN {
			sanFound = true
			break
		}
	}
	if !sanFound {
		return fmt.Errorf("SAN mismatch: expected %s, got %v (subject: %s)",
			expectedSAN, clientCert.DNSNames, clientCert.Subject.String())
	}

	return nil
}

// validateConnection is a common helper that validates client certificates from gRPC context
func (s *OCSProviderServer) validateConnection(ctx context.Context, caSecretName, expectedSAN, resourceName string) error {
	logger := klog.FromContext(ctx)

	// If secrets not configured, skip validation silently
	if caSecretName == "" || expectedSAN == "" {
		logger.Info("Client certificate validation not configured, skipping authentication", "resource", resourceName)
		return nil
	}

	// Extract client certificate from gRPC context
	p, ok := peer.FromContext(ctx)
	if !ok {
		logger.Error(nil, "Authentication failed: no peer info in context", "resource", resourceName)
		return errors.New("authentication failed")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		logger.Error(nil, "Authentication failed: no client certificate provided", "resource", resourceName)
		return errors.New("authentication failed")
	}

	clientCert := tlsInfo.State.PeerCertificates[0]

	caSecret := &corev1.Secret{}
	err := s.client.Get(ctx, types.NamespacedName{
		Name:      caSecretName,
		Namespace: s.namespace,
	}, caSecret)
	if err != nil {
		logger.Error(err, "Authentication failed: failed to get ClientCA secret",
			"secretName", caSecretName,
			"resource", resourceName)
		return errors.New("authentication failed")
	}

	caCert, ok := caSecret.Data["ca.crt"]
	if !ok || len(caCert) == 0 {
		logger.Error(nil, "Authentication failed: ca.crt not found in ClientCA secret",
			"secretName", caSecretName,
			"resource", resourceName)
		return errors.New("authentication failed")
	}

	if err := validateCACert(caCert); err != nil {
		logger.Error(err, "Authentication failed: invalid CA certificate", "resource", resourceName)
		return errors.New("authentication failed")
	}

	if err := validateClientCert(caCert, clientCert, expectedSAN); err != nil {
		logger.Error(err, "Authentication failed: client certificate validation failed", "resource", resourceName)
		return errors.New("authentication failed")
	}

	return nil
}

// authenticateClientConnection validates the client certificate against the Client CA and SAN stored in the respective StorageConsumer resource
func (s *OCSProviderServer) authenticateClientConnection(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) error {
	logger := klog.FromContext(ctx)

	// Check if authentication is explicitly disabled via annotation
	if val, exists := consumer.Annotations[DisableClientCertValidation]; exists && val == "true" {
		logger.Info("Client certificate validation explicitly disabled, skipping authentication",
			"consumer", consumer.Name)
		return nil
	}

	return s.validateConnection(ctx, consumer.Spec.ClientCASecret.Name, consumer.Spec.ClientSAN, consumer.Name)
}

// authenticatePeerConnection validates the client certificate against the Client CA and SAN stored in the respective StorageClusterPeer resource
func (s *OCSProviderServer) authenticatePeerConnection(ctx context.Context, peer *ocsv1.StorageClusterPeer) error {
	logger := klog.FromContext(ctx)

	if val, exists := peer.Annotations[DisablePeerCertValidation]; exists && val == "true" {
		logger.Info("Peer certificate validation explicitly disabled, skipping authentication",
			"peer", peer.Name)
		return nil
	}

	caSecretName := ""
	if peer.Spec.ClientCASecret != nil {
		caSecretName = peer.Spec.ClientCASecret.Name
	}

	if caSecretName == "" || peer.Spec.ClientSAN == "" {
		logger.Info("Peer certificate validation not configured, skipping authentication",
			"peer", peer.Name)
		return nil
	}

	return s.validateConnection(ctx, caSecretName, peer.Spec.ClientSAN, peer.Name)
}
