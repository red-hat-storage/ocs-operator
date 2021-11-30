package client

import (
	"context"
	"time"

	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
	"google.golang.org/grpc"
	"k8s.io/klog"
)

type ConsumerClient struct {
	Client  pb.OCSProviderClient
	timeout time.Duration
}

// NewConsumerClient creates a ConsumerClient to talk to OCS consumer server
func NewConsumerClient(cc *grpc.ClientConn, timeout time.Duration) *ConsumerClient {
	return &ConsumerClient{Client: pb.NewOCSProviderClient(cc), timeout: timeout}
}

// NewGRPCConnection returns a grpc client connection which can be used to create the consumer client
// Note: Close the connection after use
func NewGRPCConnection(serverAddr string, opts []grpc.DialOption) (*grpc.ClientConn, error) {
	conn, err := grpc.Dial(serverAddr, opts...)
	if err != nil {
		klog.Fatalf("failed to dial: %v", err)
	}
	return conn, err
}

// OnBoardConsumer RPC call to onboard a new OCS consumer cluster.
func (cc *ConsumerClient) OnBoardConsumer(consumerUUID, capacity string) (*pb.OnBoardConsumerResponse, error) {
	req := &pb.OnBoardConsumerRequest{
		StorageConsumerUUID: consumerUUID,
		Capacity:            capacity,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.OnBoardConsumer(ctx, req)
}

// OffBoardConsumer RPC call to delete the StorageConsumer CR
func (cc *ConsumerClient) OffBoardConsumer(consumerUUID string) (*pb.OffBoardConsumerResponse, error) {
	req := &pb.OffBoardConsumerRequest{
		StorageConsumerUUID: consumerUUID,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.OffBoardConsumer(ctx, req)
}

// UpdateCapacity PRC call to increase or decrease the storage pool size
func (cc *ConsumerClient) UpdateCapacity(consumerUUID, capacity string) (*pb.UpdateCapacityResponse, error) {
	req := &pb.UpdateCapacityRequest{
		StorageConsumerUUID: consumerUUID,
		Capacity:            capacity,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cc.timeout)
	defer cancel()

	return cc.Client.UpdateCapacity(ctx, req)
}
