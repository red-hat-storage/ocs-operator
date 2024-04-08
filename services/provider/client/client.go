package client

import (
	"context"
	"crypto/tls"
	"fmt"
	"time"

	ifaces "github.com/red-hat-storage/ocs-operator/v4/services/provider/interfaces"
	pb "github.com/red-hat-storage/ocs-operator/v4/services/provider/pb"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

type OCSProviderClient struct {
	Client     pb.OCSProviderClient
	clientConn *grpc.ClientConn
	timeout    time.Duration
}

// NewProviderClient creates a client to talk to the external OCS storage provider server
func NewProviderClient(ctx context.Context, serverAddr string, timeout time.Duration) (*OCSProviderClient, error) {
	apiCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	config := &tls.Config{
		InsecureSkipVerify: true,
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(credentials.NewTLS(config)),
		grpc.WithBlock(),
	}

	conn, err := grpc.DialContext(apiCtx, serverAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %v", err)
	}

	return &OCSProviderClient{
		Client:     pb.NewOCSProviderClient(conn),
		clientConn: conn,
		timeout:    timeout}, nil
}

// Close closes the gRPC connection of the external OCS storage provider client
func (cc *OCSProviderClient) Close() {
	if cc.clientConn != nil {
		_ = cc.clientConn.Close()
		cc.clientConn = nil
	}
	cc.Client = nil
}

func NewOnboardConsumerRequest() ifaces.StorageClientOnboarding {
	return &pb.OnboardConsumerRequest{}
}

// OnboardConsumer to validate the consumer and create StorageConsumer
// resource on the StorageProvider cluster
func (cc *OCSProviderClient) OnboardConsumer(ctx context.Context, onboard ifaces.StorageClientOnboarding) (*pb.OnboardConsumerResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := onboard.(*pb.OnboardConsumerRequest)

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.OnboardConsumer(apiCtx, req)
}

// GetStorageConfig generates the json config for connecting to storage provider cluster
func (cc *OCSProviderClient) GetStorageConfig(ctx context.Context, consumerUUID string) (*pb.StorageConfigResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.StorageConfigRequest{
		StorageConsumerUUID: consumerUUID,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.GetStorageConfig(apiCtx, req)
}

// OffboardConsumer deletes the StorageConsumer CR on the storage provider cluster
func (cc *OCSProviderClient) OffboardConsumer(ctx context.Context, consumerUUID string) (*pb.OffboardConsumerResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.OffboardConsumerRequest{
		StorageConsumerUUID: consumerUUID,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.OffboardConsumer(apiCtx, req)
}

func (cc *OCSProviderClient) AcknowledgeOnboarding(ctx context.Context, consumerUUID string) (*pb.AcknowledgeOnboardingResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.AcknowledgeOnboardingRequest{
		StorageConsumerUUID: consumerUUID,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.AcknowledgeOnboarding(apiCtx, req)
}

type StorageType uint

const (
	StorageTypeBlock StorageType = iota
	StorageTypeSharedfile
)

func (cc *OCSProviderClient) FulfillStorageClaim(
	ctx context.Context,
	consumerUUID string,
	storageClassClaimName string,
	storageType StorageType,
	storageProfile string,
	encryptionMethod string,
) (*pb.FulfillStorageClaimResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}
	var st pb.FulfillStorageClaimRequest_StorageType
	if storageType == StorageTypeSharedfile {
		st = pb.FulfillStorageClaimRequest_SHAREDFILE
	} else if storageType == StorageTypeBlock {
		st = pb.FulfillStorageClaimRequest_BLOCK
	}

	req := &pb.FulfillStorageClaimRequest{
		StorageConsumerUUID: consumerUUID,
		StorageClaimName:    storageClassClaimName,
		EncryptionMethod:    encryptionMethod,
		StorageType:         st,
		StorageProfile:      storageProfile,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.FulfillStorageClaim(apiCtx, req)
}

func (cc *OCSProviderClient) RevokeStorageClaim(ctx context.Context, consumerUUID, storageClassClaimName string) (*pb.RevokeStorageClaimResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.RevokeStorageClaimRequest{
		StorageConsumerUUID: consumerUUID,
		StorageClaimName:    storageClassClaimName,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.RevokeStorageClaim(apiCtx, req)
}

func (cc *OCSProviderClient) GetStorageClaimConfig(ctx context.Context, consumerUUID, storageClassClaimName string) (*pb.StorageClaimConfigResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.StorageClaimConfigRequest{
		StorageConsumerUUID: consumerUUID,
		StorageClaimName:    storageClassClaimName,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.GetStorageClaimConfig(apiCtx, req)
}

func NewStorageClientStatus() ifaces.StorageClientStatus {
	return &pb.ReportStatusRequest{}
}

func (cc *OCSProviderClient) ReportStatus(ctx context.Context, consumerUUID string, status ifaces.StorageClientStatus) (*pb.ReportStatusResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("Provider client is closed")
	}

	// panic if the request wasn't constructed using "NewStorageClientStatus()"
	req := status.(*pb.ReportStatusRequest)
	req.StorageConsumerUUID = consumerUUID
	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.ReportStatus(apiCtx, req)
}
