package client

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	ifaces "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/interfaces"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"k8s.io/apimachinery/pkg/types"
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
		// TODO fix deprecated warning
		//nolint:golint,all
		grpc.WithBlock(),
	}

	// TODO fix deprecated warning
	//nolint:golint,all
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

// GetDesiredClientState RPC call to generate the desired state of the client
func (cc *OCSProviderClient) GetDesiredClientState(ctx context.Context, consumerUUID string) (*pb.GetDesiredClientStateResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.GetDesiredClientStateRequest{
		StorageConsumerUUID: consumerUUID,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.GetDesiredClientState(apiCtx, req)
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

func (cc *OCSProviderClient) PeerStorageCluster(ctx context.Context, onboardingToken, storageClusterUID string) (*pb.PeerStorageClusterResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("OCS client is closed")
	}

	req := &pb.PeerStorageClusterRequest{
		OnboardingToken:   onboardingToken,
		StorageClusterUID: storageClusterUID,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.PeerStorageCluster(apiCtx, req)
}

func (cc *OCSProviderClient) RequestMaintenanceMode(ctx context.Context, consumerUUID string, enable bool) (*pb.RequestMaintenanceModeResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	req := &pb.RequestMaintenanceModeRequest{
		StorageConsumerUUID: consumerUUID,
		Enable:              enable,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.RequestMaintenanceMode(apiCtx, req)
}

func (cc *OCSProviderClient) GetStorageClientsInfo(ctx context.Context, storageClusterUID string, clientIDs []string) (*pb.StorageClientsInfoResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("connection to Peer OCS is closed")
	}

	req := &pb.StorageClientsInfoRequest{
		StorageClusterUID: storageClusterUID,
		ClientIDs:         clientIDs,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.GetStorageClientsInfo(apiCtx, req)
}

func (cc *OCSProviderClient) GetBlockPoolsInfo(ctx context.Context, storageClusterUID string, blockPoolNames []string) (*pb.BlockPoolsInfoResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("connection to Peer OCS is closed")
	}

	req := &pb.BlockPoolsInfoRequest{
		StorageClusterUID: storageClusterUID,
		BlockPoolNames:    blockPoolNames,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.GetBlockPoolsInfo(apiCtx, req)
}

// notifyWithReason RPC call for Notify API request
func (cc *OCSProviderClient) notifyWithReason(ctx context.Context, consumerUUID string, reason pb.NotifyReason, payload any) (*pb.NotifyResponse, error) {
	if cc.Client == nil || cc.clientConn == nil {
		return nil, fmt.Errorf("provider client is closed")
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal notify payload: %w", err)
	}

	req := &pb.NotifyRequest{
		StorageConsumerUUID: consumerUUID,
		Reason:              reason,
		Payload:             payloadBytes,
	}

	apiCtx, cancel := context.WithTimeout(ctx, cc.timeout)
	defer cancel()

	return cc.Client.Notify(apiCtx, req)
}

// NotifyObcCreated RPC call for Notify API request with OBC_CREATED reason
func (cc *OCSProviderClient) NotifyObcCreated(ctx context.Context, consumerUUID string, obc *nbv1.ObjectBucketClaim) (*pb.NotifyResponse, error) {
	return cc.notifyWithReason(ctx, consumerUUID, pb.NotifyReason_OBC_CREATED, obc)
}

// NotifyObcDeleted RPC call for Notify API request with OBC_DELETED reason
func (cc *OCSProviderClient) NotifyObcDeleted(ctx context.Context, consumerUUID string, obcDetails types.NamespacedName) (*pb.NotifyResponse, error) {
	return cc.notifyWithReason(ctx, consumerUUID, pb.NotifyReason_OBC_DELETED, obcDetails)
}
