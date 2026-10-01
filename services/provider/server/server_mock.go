package server

// Mock gRPC server infrastructure for testing

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
	"net"
	"testing"
	"time"

	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"

	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// GenerateSelfSignedCert generates a self-signed certificate for testing
func GenerateSelfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	require.NoError(t, err)

	notBefore := time.Now()
	notAfter := notBefore.Add(24 * time.Hour)

	serialNumber, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	require.NoError(t, err)

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"Test Org"},
		},
		NotBefore:             notBefore,
		NotAfter:              notAfter,
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	require.NoError(t, err)

	privBytes, err := x509.MarshalECPrivateKey(priv)
	require.NoError(t, err)

	privPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: privBytes})
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})

	cert, err := tls.X509KeyPair(certPEM, privPEM)
	require.NoError(t, err)

	return cert
}

// MockOCSProviderServer is a mock gRPC server for testing
type MockOCSProviderServer struct {
	pb.UnimplementedOCSProviderServer
	GetBlockPoolsInfoFunc     func(context.Context, *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error)
	GetStorageClientsInfoFunc func(context.Context, *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error)
}

func (m *MockOCSProviderServer) GetBlockPoolsInfo(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
	if m.GetBlockPoolsInfoFunc != nil {
		return m.GetBlockPoolsInfoFunc(ctx, req)
	}
	return &pb.BlockPoolsInfoResponse{}, nil
}

func (m *MockOCSProviderServer) GetStorageClientsInfo(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
	if m.GetStorageClientsInfoFunc != nil {
		return m.GetStorageClientsInfoFunc(ctx, req)
	}
	return &pb.StorageClientsInfoResponse{}, nil
}

// StartMockGRPCServer starts a mock gRPC server for testing with TLS
func StartMockGRPCServer(t *testing.T, mockServer *MockOCSProviderServer) (string, func()) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	cert := GenerateSelfSignedCert(t)

	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
	})

	s := grpc.NewServer(grpc.Creds(creds))
	pb.RegisterOCSProviderServer(s, mockServer)

	go func() {
		_ = s.Serve(listener)
	}()

	time.Sleep(100 * time.Millisecond)

	return listener.Addr().String(), func() {
		s.Stop()
	}
}
