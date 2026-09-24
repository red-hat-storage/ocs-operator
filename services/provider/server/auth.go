package server

import (
	"context"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"time"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
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

// authenticateConsumer validates the client certificate against the Client CA and SAN stored in the respective StorageConsumer resource
func (s *OCSProviderServer) authenticateConsumer(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) error {
	logger := klog.FromContext(ctx)

	// Extract client certificate from gRPC context
	p, ok := peer.FromContext(ctx)
	if !ok {
		logger.Error(nil, "Client authentication failed: no peer info in context", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		logger.Error(nil, "Client authentication failed: no client certificate provided", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	clientCert := tlsInfo.State.PeerCertificates[0]

	if consumer.Spec.ClientCA.Name == "" {
		logger.Error(nil, "Client authentication failed: ClientCA not configured in StorageConsumer", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	if consumer.Spec.ClientSAN == "" {
		logger.Error(nil, "Client authentication failed: ClientSAN not configured in StorageConsumer", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	caSecret := &corev1.Secret{}
	err := s.client.Get(ctx, types.NamespacedName{
		Name:      consumer.Spec.ClientCA.Name,
		Namespace: s.namespace,
	}, caSecret)
	if err != nil {
		logger.Error(err, "Client authentication failed: failed to get ClientCA secret",
			"secretName", consumer.Spec.ClientCA.Name,
			"consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	caCert, ok := caSecret.Data["ca.crt"]
	if !ok || len(caCert) == 0 {
		logger.Error(nil, "Client authentication failed: ca.crt not found in ClientCA secret",
			"secretName", consumer.Spec.ClientCA.Name,
			"consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	if err := validateCACert(caCert); err != nil {
		logger.Error(err, "Client authentication failed", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	if err := validateClientCert(caCert, clientCert, consumer.Spec.ClientSAN); err != nil {
		logger.Error(err, "Client authentication failed", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}

	return nil
}
