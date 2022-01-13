package server

import (
	"context"
	"fmt"
	"net"
	"os"

	"github.com/red-hat-storage/ocs-operator/services/provider/common"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"
	"k8s.io/klog"
)

type ocsProviderServer struct {
	pb.UnimplementedOCSProviderServer
}

// OnboardConsumer RPC call to onboard a new OCS consumer cluster.
func (c *ocsProviderServer) OnboardConsumer(ctx context.Context, req *pb.OnboardConsumerRequest) (*pb.OnboardConsumerResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockOnboardConsumer(common.MockError(mock))
	}
	return &pb.OnboardConsumerResponse{}, nil
}

// GetStorageConfig RPC call to onboard a new OCS consumer cluster.
func (c *ocsProviderServer) GetStorageConfig(ctx context.Context, req *pb.StorageConfigRequest) (*pb.StorageConfigResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockGetStorageConfig(common.MockError(mock))
	}
	return &pb.StorageConfigResponse{}, nil
}

// OffboardConsumer RPC call to delete the StorageConsumer CR
func (c *ocsProviderServer) OffboardConsumer(ctx context.Context, req *pb.OffboardConsumerRequest) (*pb.OffboardConsumerResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockOffboardConsumer(common.MockError(mock))
	}
	return &pb.OffboardConsumerResponse{}, nil
}

// UpdateCapacity PRC call to increase or decrease the storage pool size
func (c *ocsProviderServer) UpdateCapacity(ctx context.Context, req *pb.UpdateCapacityRequest) (*pb.UpdateCapacityResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockUpdateCapacity(common.MockError(mock))
	}
	return &pb.UpdateCapacityResponse{}, nil
}

func Start(port int, opts []grpc.ServerOption) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	grpcServer := grpc.NewServer(opts...)
	pb.RegisterOCSProviderServer(grpcServer, &ocsProviderServer{})
	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)
	err = grpcServer.Serve(lis)
	if err != nil {
		klog.Fatalf("failed to start gRPC server: %v", err)
	}
}
