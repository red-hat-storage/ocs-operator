package server

import (
	"context"
	"fmt"
	"net"
	"os"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/services/provider/common"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	TicketAnnotation = "ocs.openshift.io/provider-onboarding-ticket"

	ProviderCertsMountPoint = "/mnt/cert"
)

type OCSProviderServer struct {
	pb.UnimplementedOCSProviderServer
	client          client.Client
	consumerManager *ocsConsumerManager
	namespace       string
}

func NewOCSProviderServer(ctx context.Context, namespace string) (*OCSProviderServer, error) {
	client, err := newClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create new client. %v", err)
	}

	consumerManager, err := newConsumerManager(ctx, client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new OCSConumer instance. %v", err)
	}

	return &OCSProviderServer{
		client:          client,
		consumerManager: consumerManager,
		namespace:       namespace,
	}, nil
}

// OnboardConsumer RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) OnboardConsumer(ctx context.Context, req *pb.OnboardConsumerRequest) (*pb.OnboardConsumerResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockOnboardConsumer(common.MockError(mock))
	}

	// Validate capacity
	capacity, err := resource.ParseQuantity(req.Capacity)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%q is not a valid storageConsumer capacity: %v", req.Capacity, err)
	}

	// Validate onboardingTicket
	// TODO: check if ticket is already used.
	// TODO: check expiry of the ticket
	// TODO: validate ticket with public key

	storageConsumerUUID, err := s.consumerManager.Create(ctx, req.ConsumerName, req.OnboardingTicket, capacity)
	if err != nil {
		if kerrors.IsAlreadyExists(err) || err == errTicketAlreadyExists {
			return nil, status.Errorf(codes.AlreadyExists, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
		}
		return nil, status.Errorf(codes.Internal, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
	}

	// TODO: send correct granted capacity
	return &pb.OnboardConsumerResponse{StorageConsumerUUID: storageConsumerUUID, GrantedCapacity: req.Capacity}, nil
}

// GetStorageConfig RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) GetStorageConfig(ctx context.Context, req *pb.StorageConfigRequest) (*pb.StorageConfigResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockGetStorageConfig(common.MockError(mock))
	}

	// Get storage consumer resource using UUID
	consumerObj, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, err
	}

	// Verify Status
	switch consumerObj.Status.State {
	case ocsv1alpha1.StorageConsumerStateFailed:
		// TODO: get correct error message from the storageConsumer status
		return nil, status.Errorf(codes.Internal, "storageConsumer status failed")
	case ocsv1alpha1.StorageConsumerStateConfiguring:
		return nil, status.Errorf(codes.Unavailable, "waiting for the rook resources to be provisioned")
	case ocsv1alpha1.StorageConsumerStateDeleting:
		return nil, status.Errorf(codes.NotFound, "storageConsumer is already in deleting phase")
	case ocsv1alpha1.StorageConsumerStateReady:
		// TODO: Return json connection details
		return &pb.StorageConfigResponse{}, nil
	}

	return nil, status.Errorf(codes.Unavailable, "storage consumer status is not set")
}

// UpdateCapacity PRC call to increase or decrease the storage pool size
func (s *OCSProviderServer) UpdateCapacity(ctx context.Context, req *pb.UpdateCapacityRequest) (*pb.UpdateCapacityResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockUpdateCapacity(common.MockError(mock))
	}

	capacity, err := resource.ParseQuantity(req.Capacity)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%q is not a valid resource capacity: %v", req.Capacity, err)
	}

	if err := s.consumerManager.UpdateCapacity(ctx, req.StorageConsumerUUID, capacity); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "failed to update capacity in the storageConsumer resource: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to update capacity in the storageConsumer resource: %v", err)
	}

	// TODO: Return granted capacity correctly.
	return &pb.UpdateCapacityResponse{}, nil
}

// OffboardConsumer RPC call to delete the StorageConsumer CR
func (s *OCSProviderServer) OffboardConsumer(ctx context.Context, req *pb.OffboardConsumerRequest) (*pb.OffboardConsumerResponse, error) {
	mock := os.Getenv(common.MockProviderAPI)
	if mock != "" {
		return mockOffboardConsumer(common.MockError(mock))
	}

	err := s.consumerManager.Delete(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to delete storageConsumer resource with the provided UUID. %v", err)
	}

	return &pb.OffboardConsumerResponse{}, nil
}

func (s *OCSProviderServer) Start(port int, opts []grpc.ServerOption) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		klog.Fatalf("failed to listen: %v", err)
	}

	certFile := ProviderCertsMountPoint + "/tls.crt"
	keyFile := ProviderCertsMountPoint + "/tls.key"
	creds, sslErr := credentials.NewServerTLSFromFile(certFile, keyFile)
	if sslErr != nil {
		klog.Fatalf("Failed loading certificates: %v", sslErr)
		return
	}

	opts = append(opts, grpc.Creds(creds))
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterOCSProviderServer(grpcServer, s)
	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)
	err = grpcServer.Serve(lis)
	if err != nil {
		klog.Fatalf("failed to start gRPC server: %v", err)
	}
}

func newClient() (client.Client, error) {
	scheme := runtime.NewScheme()
	err := ocsv1alpha1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add ocsv1alpha1 to scheme. %v", err)
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add ocsv1alpha1 to scheme. %v", err)
	}

	config, err := config.GetConfig()
	if err != nil {
		klog.Error(err, "failed to get rest.config")
		return nil, err
	}
	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		klog.Error(err, "failed to create controller-runtime client")
		return nil, err
	}

	return client, nil
}
