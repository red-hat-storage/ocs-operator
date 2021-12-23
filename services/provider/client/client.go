package client

import (
	"context"
	"fmt"
	"time"

	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
	"google.golang.org/grpc"
)

type OCSProviderClient struct {
	Client  pb.OCSProviderClient
	timeout time.Duration
}

// NewProviderClient creates a client to talk to OCS provider server
func NewProviderClient(cc *grpc.ClientConn, timeout time.Duration) *OCSProviderClient {
	return &OCSProviderClient{Client: pb.NewOCSProviderClient(cc), timeout: timeout}
}

// NewGRPCConnection returns a grpc client connection which can be used to create the consumer client
// Note: Close the connection after use
func NewGRPCConnection(serverAddr string, opts []grpc.DialOption) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to dial: %v", err)
	}
	return conn, err
}

// OnboardConsumer to validate the consumer and create StorageConsumer
// resource on the StorageProvider cluster
func (cc *OCSProviderClient) OnboardConsumer(token, name, capacity string) (*pb.OnboardConsumerResponse, error) {
	req := &pb.OnboardConsumerRequest{
		Token:        token,
		ConsumerName: name,
		Capacity:     capacity,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.OnboardConsumer(ctx, req)
}

// GetStorageConfig generates the json config for connecting to storage provider cluster
func (cc *OCSProviderClient) GetStorageConfig(consumerUUID string) (*pb.StorageConfigResponse, error) {
	req := &pb.StorageConfigRequest{
		StorageConsumerUUID: consumerUUID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.GetStorageConfig(ctx, req)
}

// OffboardConsumer deletes the StorageConsumer CR on the storage provider cluster
func (cc *OCSProviderClient) OffboardConsumer(consumerUUID string) (*pb.OffboardConsumerResponse, error) {
	req := &pb.OffboardConsumerRequest{
		StorageConsumerUUID: consumerUUID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.OffboardConsumer(ctx, req)
}

// UpdateCapacity increases or decreases the storage block pool size
func (cc *OCSProviderClient) UpdateCapacity(consumerUUID, capacity string) (*pb.UpdateCapacityResponse, error) {
	req := &pb.UpdateCapacityRequest{
		StorageConsumerUUID: consumerUUID,
		Capacity:            capacity,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.UpdateCapacity(ctx, req)
}
