package server

import (
	"context"
	"crypto/x509"
	"errors"
	"fmt"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/peer"
	corev1 "k8s.io/api/core/v1"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// validateClientCert validates a client certificate against a CA, intermediates, and expected SAN.
func validateClientCert(caCert []byte, clientCert *x509.Certificate, intermediates *x509.CertPool, expectedSAN string) error {
	caCertPool := x509.NewCertPool()
	if !caCertPool.AppendCertsFromPEM(caCert) {
		return errors.New("failed to parse CA certificate for verification")
	}

	opts := x509.VerifyOptions{
		Roots:         caCertPool,
		Intermediates: intermediates,
		KeyUsages:     []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if _, err := clientCert.Verify(opts); err != nil {
		return fmt.Errorf("certificate verification failed (subject: %s, issuer: %s): %w",
			clientCert.Subject.String(), clientCert.Issuer.String(), err)
	}

	for _, san := range clientCert.DNSNames {
		if san == expectedSAN {
			return nil
		}
	}

	return fmt.Errorf("SAN mismatch: expected %s, got %v (subject: %s)",
		expectedSAN, clientCert.DNSNames, clientCert.Subject.String())
}

// validateConnection validates client certificates from gRPC context.
func (s *OCSProviderServer) validateConnection(ctx context.Context, caConfigMapName, expectedSAN, resourceName string) error {
	// If configmap not configured, skip validation
	// TODO: Mandate certificate validation for all connections in future releases.
	if caConfigMapName == "" || expectedSAN == "" {
		return nil
	}

	// Extract client certificate from gRPC context
	p, ok := peer.FromContext(ctx)
	if !ok {
		return fmt.Errorf("no peer info in gRPC context for %s", resourceName)
	}

	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok || len(tlsInfo.State.PeerCertificates) == 0 {
		return fmt.Errorf("no client certificate provided for %s", resourceName)
	}

	clientCert := tlsInfo.State.PeerCertificates[0]

	intermediatePool := x509.NewCertPool()
	for i := 1; i < len(tlsInfo.State.PeerCertificates); i++ {
		intermediatePool.AddCert(tlsInfo.State.PeerCertificates[i])
	}

	caConfigMap := &corev1.ConfigMap{}
	caConfigMap.Name = caConfigMapName
	caConfigMap.Namespace = s.namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(caConfigMap), caConfigMap); err != nil {
		return fmt.Errorf("failed to get ClientCA configmap %s for %s: %w", caConfigMapName, resourceName, err)
	}

	caCertStr, ok := caConfigMap.Data["ca.crt"]
	if !ok || len(caCertStr) == 0 {
		return fmt.Errorf("ca.crt not found in configmap %s for %s", caConfigMapName, resourceName)
	}

	caCert := []byte(caCertStr)

	if err := validateClientCert(caCert, clientCert, intermediatePool, expectedSAN); err != nil {
		return fmt.Errorf("client certificate validation failed for %s: %w", resourceName, err)
	}

	return nil
}

// authenticateClientConnection validates the client certificate against the Client CA and SAN stored in the respective StorageConsumer resource.
func (s *OCSProviderServer) authenticateClientConnection(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) error {
	if err := s.validateConnection(ctx, consumer.Spec.ClientCAConfigMap.Name, consumer.Spec.ClientSAN, consumer.Name); err != nil {
		logger := klog.FromContext(ctx)
		logger.Error(err, "Authentication failed", "consumer", consumer.Name)
		return errors.New("authentication failed")
	}
	return nil
}
