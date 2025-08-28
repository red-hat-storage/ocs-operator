package server

import (
	"cmp"
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	ocsVersion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	csiopv1 "github.com/ceph/ceph-csi-operator/api/v1"
	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	"github.com/go-logr/logr"
	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	nbapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ProviderCertsMountPoint   = "/mnt/cert"
	onboardingTicketKeySecret = "onboarding-ticket-key"
	notAvailable              = "N/A"

	oneGibInBytes                 = 1024 * 1024 * 1024
	monConfigMap                  = "rook-ceph-mon-endpoints"
	monSecret                     = "rook-ceph-mon"
	mirroringTokenKey             = "rbdMirrorBootstrapPeerSecretName"
	clientInfoRbdClientProfileKey = "csiop-rbd-client-profile"
	csiCephUserCurrGen            = 1
)

var (
	knownOcsInstalledCsvName        string
	knownOcsSubscriptionChannelName string
	ocsSubChannelAndCsvMutex        sync.Mutex
)

type CommonClassSpecAccessors interface {
	GetName() string
	GetRename() string
	GetAliases() []string
}

type OCSProviderServer struct {
	pb.UnimplementedOCSProviderServer
	client                    client.Client
	scheme                    *runtime.Scheme
	consumerManager           *ocsConsumerManager
	storageClusterPeerManager *storageClusterPeerManager
	namespace                 string
}

func NewOCSProviderServer(ctx context.Context, namespace string) (*OCSProviderServer, error) {
	scheme, err := newScheme()
	if err != nil {
		return nil, fmt.Errorf("failed to create new scheme. %v", err)
	}

	client, err := util.NewK8sClient(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to create new client. %v", err)
	}

	consumerManager, err := newConsumerManager(ctx, client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new OCSConumer instance. %v", err)
	}

	storageClusterPeerManager, err := newStorageClusterPeerManager(client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new StorageClusterPeer instance. %v", err)
	}

	return &OCSProviderServer{
		client:                    client,
		scheme:                    scheme,
		consumerManager:           consumerManager,
		storageClusterPeerManager: storageClusterPeerManager,
		namespace:                 namespace,
	}, nil
}

// OnboardConsumer RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) OnboardConsumer(ctx context.Context, req *pb.OnboardConsumerRequest) (*pb.OnboardConsumerResponse, error) {
	logger := klog.FromContext(ctx).WithName("OnboardConsumer")
	logger.Info("Starting OnboardConsumer RPC", "request", req)

	version, err := semver.FinalizeVersion(req.ClientOperatorVersion)
	if err != nil {
		logger.Error(err, "Malformed ClientOperatorVersion provided", "clientOperatorVersion", req.ClientOperatorVersion)
		return nil, status.Errorf(codes.InvalidArgument, "malformed ClientOperatorVersion for client %q is provided. %v", req.ConsumerName, err)
	}

	serverVersion, _ := semver.Make(ocsVersion.Version)
	clientVersion, _ := semver.Make(version)
	if serverVersion.Major != clientVersion.Major || serverVersion.Minor != clientVersion.Minor {
		logger.Error(
			fmt.Errorf("version mismatch"),
			"Server and client operator versions do not match",
			"server version", serverVersion,
			"client version", clientVersion,
		)
		return nil, status.Errorf(codes.FailedPrecondition, "both server and client %q operators major and minor versions should match for onboarding process", req.ConsumerName)
	}

	pubKey, err := s.getOnboardingValidationKey(ctx)
	if err != nil {
		logger.Error(err, "Failed to get public key to validate onboarding ticket")
		return nil, status.Errorf(codes.Internal, "failed to get public key to validate onboarding ticket for consumer %q. %v", req.ConsumerName, err)
	}

	onboardingTicket, err := decodeAndValidateTicket(logger, req.OnboardingTicket, pubKey)
	if err != nil {
		logger.Error(err, "Failed to validate onboarding ticket")
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket is not valid. %v", err)
	}

	if onboardingTicket.SubjectRole != services.ClientRole {
		err := fmt.Errorf("invalid onboarding ticket role: expecting %s found %s", services.ClientRole, onboardingTicket.SubjectRole)
		logger.Error(err, "Invalid onboarding ticket role", "expectedRole", services.ClientRole, "actualRole", onboardingTicket.SubjectRole)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	storageConsumer := &ocsv1alpha1.StorageConsumer{}
	storageConsumer.Name = onboardingTicket.ID
	storageConsumer.Namespace = s.namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); err != nil {
		logger.Error(err, "Failed to get StorageConsumer referred by the supplied token", "storageConsumerName", storageConsumer.Name)
		return nil, status.Errorf(codes.Internal, "failed to get storageconsumer. %v", err)
	} else if storageConsumer.Spec.Enable {
		if storageConsumer.Status.Client != nil && storageConsumer.Status.Client.ID == req.ClientID {
			fillStorageClientInfo(&storageConsumer.Status, req)
			if err := s.client.Status().Update(ctx, storageConsumer); err != nil {
				return nil, status.Errorf(codes.Internal, "failed to update storageconsumer status: %v", err)
			}
			return &pb.OnboardConsumerResponse{StorageConsumerUUID: string(storageConsumer.UID)}, nil
		}
		logger.Error(fmt.Errorf("StorageConsumer already enabled"), "refusing to onboard onto storageconsumer with supplied token", "storageConsumerName", storageConsumer.Name)
		return nil, status.Errorf(codes.InvalidArgument, "refusing to onboard onto storageconsumer with supplied token")
	}

	if err := checkTicketExpiration(logger, onboardingTicket); err != nil {
		logger.Error(err, "onboarding ticket expired for consumer")
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket is expired. %v", err)
	}

	onboardingSecret := &corev1.Secret{}
	onboardingSecret.Name = fmt.Sprintf("onboarding-token-%s", storageConsumer.UID)
	onboardingSecret.Namespace = s.namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(onboardingSecret), onboardingSecret); err != nil {
		logger.Error(err, "Failed to get onboarding secret corresponding to StorageConsumer", "secretName", onboardingSecret.Name, "storageConsumerName", storageConsumer.Name)
		return nil, status.Errorf(codes.Internal, "failed to get onboarding secret. %v", err)
	}
	if req.OnboardingTicket != string(onboardingSecret.Data[defaults.OnboardingTokenKey]) {
		logger.Error(fmt.Errorf("ticket mismatch"), "Supplied onboarding ticket does not match StorageConsumer secret", "secretName", onboardingSecret.Name)
		return nil, status.Errorf(codes.InvalidArgument, "supplied onboarding ticket does not match mapped secret")
	}

	storageConsumerUUID, err := s.consumerManager.EnableStorageConsumer(ctx, storageConsumer, req)
	if err != nil {
		logger.Error(err, "Failed to onboard StorageConsumer resource", "storageConsumerName", storageConsumer.Name)
		return nil, status.Errorf(codes.Internal, "failed to onboard on storageConsumer resource. %v", err)
	}

	logger.Info("Successfully onboarded consumer", "storageConsumerUUID", storageConsumerUUID, "storageConsumerName", storageConsumer.Name)
	return &pb.OnboardConsumerResponse{StorageConsumerUUID: storageConsumerUUID}, nil
}

// GetDesiredClientState RPC call to generate the desired state of the client
func (s *OCSProviderServer) GetDesiredClientState(ctx context.Context, req *pb.GetDesiredClientStateRequest) (*pb.GetDesiredClientStateResponse, error) {
	logger := klog.FromContext(ctx).WithName("GetDesiredClientState").WithValues("consumer", req.StorageConsumerUUID)
	logger.Info("Starting GetDesiredClientState RPC", "request", req)

	// Get storage consumer resource using UUID
	consumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		logger.Error(err, "failed to get consumer")
		return nil, status.Errorf(codes.Internal, "failed to get StorageConsumer")
	}

	logger.Info("Found StorageConsumer for GetDesiredClientState", "StorageConsumer", consumer.Name)
	if !checkClientPreConditions(consumer, ocsVersion.Version, logger) {
		return nil, status.Error(codes.FailedPrecondition, "client operator does not meet version requirements")
	}

	// Verify Status
	switch consumer.Status.State {
	case ocsv1alpha1.StorageConsumerStateNotEnabled:
		return nil, status.Errorf(codes.FailedPrecondition, "StorageConsumer is not yet enabled")
	case ocsv1alpha1.StorageConsumerStateFailed:
		return nil, status.Errorf(codes.Internal, "StorageConsumer status failed")
	case ocsv1alpha1.StorageConsumerStateConfiguring:
		return nil, status.Errorf(codes.Unavailable, "waiting for the resources to be provisioned")
	case ocsv1alpha1.StorageConsumerStateDeleting:
		return nil, status.Errorf(codes.NotFound, "StorageConsumer is in deleting phase")
	case ocsv1alpha1.StorageConsumerStateReady:
		kubeResources, err := s.getKubeResources(ctx, logger, consumer)
		if err != nil {
			logger.Error(err, "failed to get kube resources")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		response := &pb.GetDesiredClientStateResponse{}

		for _, kubeResource := range kubeResources {
			gvk, err := apiutil.GVKForObject(kubeResource, s.scheme)
			if err != nil {
				return nil, err
			}
			if kubeResource.GetName() == "" {
				logger.Error(fmt.Errorf("invalid resource"), "resource is missing a name", "kubeResource", kubeResource)
				return nil, status.Errorf(codes.Internal, "failed to produce client state.")
			}

			// FIXME: Remove this once we drop csi operator v1alpha1 APIs
			clientSemver, err := semver.Parse(consumer.Status.Client.OperatorVersion)
			if err != nil {
				logger.Error(err, "failed to parse client operator version")
				return nil, status.Errorf(codes.Internal, "failed to produce client state.")
			}
			if clientSemver.Major == 4 && clientSemver.Minor <= 19 &&
				gvk.Group == csiopv1.GroupVersion.Group {
				gvk.Version = "v1alpha1"
			}

			kubeResource.GetObjectKind().SetGroupVersionKind(gvk)
			sanitizeKubeResource(kubeResource)
			kubeResourceBytes := util.JsonMustMarshal(kubeResource)
			response.KubeObjects = append(response.KubeObjects, &pb.KubeObject{Bytes: kubeResourceBytes})
		}

		channelName, err := s.getOCSSubscriptionChannel(ctx)
		if err != nil {
			logger.Error(err, "failed to get channel name for Client Operator")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.ClientOperatorChannel = channelName

		inMaintenanceMode, err := s.isSystemInMaintenanceMode(ctx)
		if err != nil {
			logger.Error(err, "failed to produce client state")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.MaintenanceMode = inMaintenanceMode

		isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, consumer)
		if err != nil {
			logger.Error(err, "failed to produce client state")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.MirrorEnabled = isConsumerMirrorEnabled

		storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
		if err != nil {
			return nil, err
		}

		cephConnection, err := s.getDesiredCephConnection(ctx, consumer, storageCluster)
		if err != nil {
			logger.Error(err, "failed to produce client state")
			return nil, status.Errorf(codes.Internal, "failed to produce client state hash")
		}

		availableServices, err := util.GetAvailableServices(ctx, s.client, storageCluster)
		if err != nil {
			logger.Error(err, "failed to get available services")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		if availableServices.Rbd {
			response.RbdDriverRequirements = &pb.RbdDriverRequirements{}
		}
		if availableServices.CephFs {
			response.CephFsDriverRequirements = &pb.CephFsDriverRequirements{}
		}
		if availableServices.Nfs {
			response.NfsDriverRequirements = &pb.NfsDriverRequirements{}
		}

		topologyKey := consumer.GetAnnotations()[util.AnnotationNonResilientPoolsTopologyKey]
		if topologyKey != "" {
			response.RbdDriverRequirements = &pb.RbdDriverRequirements{
				TopologyDomainLables: []string{topologyKey},
			}
		}

		desiredClientConfigHash := getDesiredClientConfigHash(
			channelName,
			consumer,
			cephConnection.Spec,
			isEncryptionInTransitEnabled(storageCluster.Spec.Network),
			inMaintenanceMode,
			isConsumerMirrorEnabled,
			topologyKey,
			ocsVersion.Version,
			availableServices.Rbd,
			availableServices.CephFs,
			availableServices.Nfs,
		)
		response.DesiredStateHash = desiredClientConfigHash

		logger.Info("successfully returned the config details to the client")
		return response, nil
	}

	return nil, status.Errorf(codes.Unavailable, "StorageConsumer status is not set")

}

// OffboardConsumer RPC call to delete the StorageConsumer CR
func (s *OCSProviderServer) OffboardConsumer(ctx context.Context, req *pb.OffboardConsumerRequest) (*pb.OffboardConsumerResponse, error) {
	logger := klog.FromContext(ctx).WithName("OffboardConsumer").WithValues("consumer", req.StorageConsumerUUID)
	logger.Info("Starting OffboardConsumer RPC", "request", req)
	err := s.consumerManager.ClearClientInformation(ctx, req.StorageConsumerUUID)
	if err != nil {
		logger.Error(err, "failed to clear client information")
		return nil, status.Errorf(codes.Internal, "failed to offboard storageConsumer with the provided UUID. %v", err)
	}
	logger.Info("Successfully Offboarded Client from StorageConsumer with the provided UUID")
	return &pb.OffboardConsumerResponse{}, nil
}

func (s *OCSProviderServer) Start(port int, opts []grpc.ServerOption) {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		log.Log.Error(err, "failed to listen")
		os.Exit(1)
	}

	certFile := ProviderCertsMountPoint + "/tls.crt"
	keyFile := ProviderCertsMountPoint + "/tls.key"
	creds, sslErr := credentials.NewServerTLSFromFile(certFile, keyFile)
	if sslErr != nil {
		log.Log.Error(sslErr, "failed loading certificates")
		os.Exit(1)
	}

	opts = append(opts, grpc.Creds(creds))
	grpcServer := grpc.NewServer(opts...)
	pb.RegisterOCSProviderServer(grpcServer, s)
	// Register reflection service on gRPC server.
	reflection.Register(grpcServer)
	err = grpcServer.Serve(lis)
	if err != nil {
		log.Log.Error(err, "failed to start gRPC server")
		os.Exit(1)
	}
}

func newScheme() (*runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	err := ocsv1alpha1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add ocsv1alpha1 to scheme. %v", err)
	}
	if err = corev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add corev1 to scheme. %v", err)
	}
	if err = rookCephv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add rookCephv1 to scheme. %v", err)
	}
	if err = opv1a1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add operatorsv1alpha1 to scheme. %v", err)
	}
	if err = ocsv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add ocsv1 to scheme. %v", err)
	}
	if err = routev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add routev1 to scheme. %v", err)
	}
	if err = templatev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add templatev1 to scheme. %v", err)
	}
	if err = csiopv1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add csiopv1 to scheme. %v", err)
	}
	if err = storagev1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add storagev1 to scheme. %v", err)
	}
	if err = snapapi.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add snapapi to scheme. %v", err)
	}
	if err = groupsnapapi.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add groupsnapapi to scheme. %v", err)
	}
	if err = odfgsapiv1b1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add odfgsapiv1b1 to scheme. %v", err)
	}
	if err = replicationv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add replicationv1alpha1 to scheme. %v", err)
	}
	if err = quotav1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add quotav1 to scheme. %v", err)
	}
	if err = nbapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add nbapis to scheme. %v", err)
	}
	if err = csiaddonsv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add nbapis to scheme. %v", err)
	}

	return scheme, nil
}

func (s *OCSProviderServer) getOnboardingValidationKey(ctx context.Context) (*rsa.PublicKey, error) {
	pubKeySecret := &corev1.Secret{}
	err := s.client.Get(ctx, types.NamespacedName{Name: onboardingTicketKeySecret, Namespace: s.namespace}, pubKeySecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get public key secret %q", onboardingTicketKeySecret)
	}

	pubKeyBytes := pubKeySecret.Data["key"]
	if len(pubKeyBytes) == 0 {
		return nil, fmt.Errorf("public key is not found inside the secret %q", onboardingTicketKeySecret)
	}

	block, _ := pem.Decode(pubKeyBytes)
	if block == nil {
		return nil, fmt.Errorf("invalid PEM block")
	}

	publicKey, err := x509.ParsePKCS1PublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key. %v", err)
	}

	return publicKey, nil
}

func decodeAndValidateTicket(logger logr.Logger, ticket string, pubKey *rsa.PublicKey) (*services.OnboardingTicket, error) {
	ticketArr := strings.Split(string(ticket), ".")
	if len(ticketArr) != 2 {
		return nil, fmt.Errorf("invalid ticket")
	}

	message, err := base64.StdEncoding.DecodeString(ticketArr[0])
	if err != nil {
		return nil, fmt.Errorf("failed to decode onboarding ticket: %v", err)
	}

	var ticketData services.OnboardingTicket
	err = json.Unmarshal(message, &ticketData)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal onboarding ticket message. %v", err)
	}

	switch ticketData.SubjectRole {
	case services.ClientRole, services.PeerRole:
	default:
		return nil, fmt.Errorf("invalid onboarding ticket subject role")
	}

	signature, err := base64.StdEncoding.DecodeString(ticketArr[1])
	if err != nil {
		return nil, fmt.Errorf("failed to decode onboarding ticket %s signature: %v", ticketData.ID, err)
	}

	hash := sha256.Sum256(message)
	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
	if err != nil {
		return nil, fmt.Errorf("failed to verify onboarding ticket signature. %v", err)
	}

	logger.Info("onboarding ticket %s has been verified successfully", ticketData.ID)
	return &ticketData, nil
}

func checkTicketExpiration(logger logr.Logger, ticketData *services.OnboardingTicket) error {
	if ticketData.ExpirationDate < time.Now().Unix() {
		return fmt.Errorf("onboarding ticket %s is expired", ticketData.ID)
	}
	logger.Info("onboarding ticket for %s is valid", ticketData.ID)
	return nil
}

// ReportStatus rpc call to check if a consumer can reach to the provider.
func (s *OCSProviderServer) ReportStatus(ctx context.Context, req *pb.ReportStatusRequest) (*pb.ReportStatusResponse, error) {
	logger := klog.FromContext(ctx).WithName("ReportStatus").WithValues("consumer", req.StorageConsumerUUID)
	logger.Info("Processing status report", "request", req)

	if req.ClientOperatorVersion == "" {
		req.ClientOperatorVersion = notAvailable
	} else {
		if _, err := semver.Parse(req.ClientOperatorVersion); err != nil {
			logger.Error(err, "Malformed ClientOperatorVersion", "clientOperatorVersion", req.ClientOperatorVersion)
			return nil, status.Errorf(codes.InvalidArgument, "Malformed ClientOperatorVersion: %v", err)
		}
	}

	if req.ClientPlatformVersion == "" {
		req.ClientPlatformVersion = notAvailable
	} else {
		if _, err := semver.Parse(req.ClientPlatformVersion); err != nil {
			logger.Error(err, "Malformed ClientPlatformVersion", "clientPlatformVersion", req.ClientPlatformVersion)
			return nil, status.Errorf(codes.InvalidArgument, "Malformed ClientPlatformVersion: %v", err)
		}
	}

	if err := s.consumerManager.UpdateConsumerStatus(ctx, req.StorageConsumerUUID, req); err != nil {
		if kerrors.IsNotFound(err) {
			logger.Error(err, "StorageConsumer not found when updating status")
			return nil, status.Errorf(codes.NotFound, "Failed to update lastHeartbeat payload in the storageConsumer resource: %v", err)
		}
		logger.Error(err, "Failed to update consumer status")
		return nil, status.Errorf(codes.Internal, "Failed to update lastHeartbeat payload in the storageConsumer resource: %v", err)
	}

	storageConsumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		logger.Error(err, "Failed to get StorageConsumer resource")
		return nil, status.Errorf(codes.Internal, "Failed to get storageConsumer resource: %v", err)
	}

	channelName, err := s.getOCSSubscriptionChannel(ctx)
	if err != nil {
		logger.Error(err, "Failed to get OCS subscription channel")
		return nil, status.Errorf(codes.Internal, "Failed to construct status response: %v", err)
	}

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		logger.Error(err, "Failed to get StorageCluster")
		return nil, err
	}

	inMaintenanceMode, err := s.isSystemInMaintenanceMode(ctx)
	if err != nil {
		logger.Error(err, "Failed to get maintenance mode status")
		return nil, status.Errorf(codes.Internal, "Failed to get maintenance mode status.")
	}

	isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, storageConsumer)
	if err != nil {
		logger.Error(err, "Failed to get mirroring status for consumer")
		return nil, status.Errorf(codes.Internal, "Failed to get mirroring status for consumer.")
	}

	cephConnection, err := s.getDesiredCephConnection(ctx, storageConsumer, storageCluster)
	if err != nil {
		logger.Error(err, "Failed to get desired Ceph connection")
		return nil, status.Errorf(codes.Internal, "failed to produce client state hash")
	}

	topologyKey := storageConsumer.GetAnnotations()[util.AnnotationNonResilientPoolsTopologyKey]

	availableServices, err := util.GetAvailableServices(ctx, s.client, storageCluster)
	if err != nil {
		logger.Error(err, "Failed to get available services")
		return nil, status.Errorf(codes.Internal, "Failed to produce client state hash")
	}

	desiredClientConfigHash := getDesiredClientConfigHash(
		channelName,
		storageConsumer,
		cephConnection.Spec,
		isEncryptionInTransitEnabled(storageCluster.Spec.Network),
		inMaintenanceMode,
		isConsumerMirrorEnabled,
		topologyKey,
		ocsVersion.Version,
		availableServices.Rbd,
		availableServices.CephFs,
		availableServices.Nfs,
	)

	logger.Info("Successfully processed status report")
	return &pb.ReportStatusResponse{
		DesiredClientOperatorChannel: channelName,
		DesiredConfigHash:            desiredClientConfigHash,
	}, nil
}

func getDesiredClientConfigHash(parts ...any) string {
	return util.CalculateMD5Hash(parts)
}

func (s *OCSProviderServer) getOCSSubscriptionChannel(ctx context.Context) (string, error) {
	subscriptionList := &opv1a1.SubscriptionList{}
	err := s.client.List(ctx, subscriptionList, client.InNamespace(s.namespace))
	if err != nil {
		return "", err
	}
	subscription := util.Find(subscriptionList.Items, func(sub *opv1a1.Subscription) bool {
		return sub.Spec.Package == "ocs-operator"
	})
	if subscription == nil {
		klog.Info("unable to find ocs-operator subscription")
		return "", nil
	}

	ocsSubChannelAndCsvMutex.Lock()
	defer ocsSubChannelAndCsvMutex.Unlock()

	// If the installed CSV has changed then validate the current channel otherwise return the known valid channel
	if subscription.Status.InstalledCSV != knownOcsInstalledCsvName {
		ok, err := s.isSubscriptionChannelValid(ctx, subscription)
		if err != nil || !ok {
			return "", err
		}
		knownOcsInstalledCsvName = subscription.Status.InstalledCSV
		knownOcsSubscriptionChannelName = subscription.Spec.Channel
	}

	return knownOcsSubscriptionChannelName, nil
}

func (s *OCSProviderServer) isSubscriptionChannelValid(ctx context.Context, subscription *opv1a1.Subscription) (bool, error) {
	installedCSVName := subscription.Status.InstalledCSV
	channelName := subscription.Spec.Channel
	packageName := subscription.Spec.Package

	// Check if the installed CSV of the subscription is in Succeeded phase
	if installedCSVName == "" {
		klog.Infof("subscription %q does not have an installed CSV", subscription.Name)
		return false, nil
	}
	csv := &opv1a1.ClusterServiceVersion{}
	err := s.client.Get(ctx, client.ObjectKey{Name: installedCSVName, Namespace: s.namespace}, csv)
	if err != nil {
		klog.Errorf("failed to get installed CSV %q: %v", installedCSVName, err)
		return false, err
	}
	if csv.Status.Phase != opv1a1.CSVPhaseSucceeded {
		klog.Infof("installed CSV %q is in phase %q, not Succeeded", csv.Name, csv.Status.Phase)
		return false, nil
	}

	// Check if the installed CSV is the expected one for the channel from the package manifest
	packageManifestList := &unstructured.UnstructuredList{}
	packageManifestList.SetGroupVersionKind(schema.GroupVersionKind{
		Group:   "packages.operators.coreos.com",
		Version: "v1",
		Kind:    "PackageManifest",
	})
	err = s.client.List(
		ctx,
		packageManifestList,
		client.InNamespace(s.namespace),
		// Field match prone to future change, but better than processing hundreds of resources, consuming more CPU/memory
		client.MatchingFields{"metadata.name": packageName},
	)
	if err != nil {
		klog.Errorf("failed to list packageManifests for package %q: %v", packageName, err)
		return false, err
	}
	var packageManifest *unstructured.Unstructured
	for i := range packageManifestList.Items {
		pm := &packageManifestList.Items[i]
		labels := pm.GetLabels()
		if labels["catalog"] == subscription.Spec.CatalogSource &&
			labels["catalog-namespace"] == subscription.Spec.CatalogSourceNamespace {
			packageManifest = pm
			break
		}
	}
	if packageManifest == nil {
		klog.Errorf("PackageManifest %q not found for catalog %q in namespace %q",
			packageName, subscription.Spec.CatalogSource, subscription.Spec.CatalogSourceNamespace)
		return false, nil
	}
	channels, found, err := unstructured.NestedSlice(packageManifest.Object, "status", "channels")
	if err != nil {
		klog.Errorf("error getting channels from PackageManifest %q: %v", packageManifest.GetName(), err)
		return false, err
	}
	if !found {
		klog.Infof("channels not found in PackageManifest %q", packageManifest.GetName())
		return false, nil
	}
	var expectedCSVName string
	for _, ch := range channels {
		chMap, ok := ch.(map[string]any)
		if !ok {
			continue
		}
		if chMap["name"].(string) == channelName {
			expectedCSVName, ok = chMap["currentCSV"].(string)
			if !ok {
				klog.Infof("currentCSV not found or invalid in channel %s", channelName)
				return false, nil
			}
			break
		}
	}
	if installedCSVName != expectedCSVName {
		klog.Infof("installed CSV %q does not match expected %q", installedCSVName, expectedCSVName)
		return false, nil
	}

	klog.Infof("subscription for %q with channel %q is valid with installed CSV %q", subscription.Spec.Package, channelName, installedCSVName)
	return true, nil
}

func isEncryptionInTransitEnabled(networkSpec *rookCephv1.NetworkSpec) bool {
	return networkSpec != nil &&
		networkSpec.Connections != nil &&
		networkSpec.Connections.Encryption != nil &&
		networkSpec.Connections.Encryption.Enabled
}

func extractMonitorIps(data string) ([]string, error) {
	var ips []string
	mons := strings.Split(data, ",")
	for _, mon := range mons {
		parts := strings.Split(mon, "=")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid mon ip format: %s", mon)
		}
		ips = append(ips, parts[1])
	}
	// sorting here removes any positional change which reduces spurious reconciles
	slices.Sort(ips)

	// Rook does not update the rook-ceph-mon-endpoint ConfigMap until mons failover
	// Starting from 4.18, RequireMsgr2 is always enabled, and encryption in transit is allowed on existing clusters.
	// So, we need to replace the msgr1 port with msgr2 port.
	replaceMsgr1PortWithMsgr2(ips)

	return ips, nil
}

func replaceMsgr1PortWithMsgr2(ips []string) {
	const (
		// msgr2port is the listening port of the messenger v2 protocol
		msgr2port = "3300"
		// msgr1port is the listening port of the messenger v1 protocol
		msgr1port = "6789"
	)

	for i, ip := range ips {
		if strings.HasSuffix(ip, msgr1port) {
			ips[i] = strings.TrimSuffix(ip, msgr1port) + msgr2port
		}
	}
}

func (s *OCSProviderServer) PeerStorageCluster(ctx context.Context, req *pb.PeerStorageClusterRequest) (*pb.PeerStorageClusterResponse, error) {
	logger := klog.FromContext(ctx).WithName("PeerStorageCluster").WithValues("storage cluster", req.StorageClusterUID)
	logger.Info("Starting PeerStorageCluster RPC", "request", req)

	pubKey, err := s.getOnboardingValidationKey(ctx)
	if err != nil {
		logger.Error(err, "failed to get public key to validate peer onboarding ticket")
		return nil, status.Errorf(codes.Internal, "failed to validate peer onboarding ticket")
	}

	onboardingToken, err := decodeAndValidateTicket(logger, req.OnboardingToken, pubKey)
	if err != nil {
		logger.Error(err, "failed to validate onboarding ticket signature")
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket signature is not valid. %v", err)
	}

	if err := checkTicketExpiration(logger, onboardingToken); err != nil {
		logger.Error(err, "onboarding ticket expired")
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket is expired. %v", err)
	}

	if onboardingToken.SubjectRole != services.PeerRole {
		err := fmt.Errorf("invalid onboarding ticket for %q, expecting role %s found role %s", req.StorageClusterUID, services.PeerRole, onboardingToken.SubjectRole)
		logger.Error(err, "invalid onboarding ticket role")
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	storageClusterPeer, err := s.storageClusterPeerManager.GetByPeerStorageClusterUID(ctx, types.UID(req.StorageClusterUID))
	if err != nil {
		logger.Error(err, "failed to get storage cluster peer that meets the criteria")
		return nil, status.Errorf(codes.NotFound, "Cannot find a storage cluster peer that meets all criteria")
	}

	logger.Info("Found StorageClusterPeer", "storageClusterPeer", storageClusterPeer.Name)

	if storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePending && storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePeered {
		return nil, status.Errorf(codes.NotFound, "Cannot find a storage cluster peer that meets all criteria")
	}

	logger.Info("Successfully returned from PeerStorageCluster")
	return &pb.PeerStorageClusterResponse{}, nil
}

func (s *OCSProviderServer) RequestMaintenanceMode(ctx context.Context, req *pb.RequestMaintenanceModeRequest) (*pb.RequestMaintenanceModeResponse, error) {
	logger := klog.FromContext(ctx).WithName("RequestMaintenanceMode").WithValues("consumer", req.StorageConsumerUUID)
	logger.Info("Starting RequestMaintenanceMode RPC", "request", req)

	// Get storage consumer resource using UUID
	if req.Enable {
		err := s.consumerManager.AddAnnotation(ctx, req.StorageConsumerUUID, util.RequestMaintenanceModeAnnotation, "")
		if err != nil {
			logger.Error(err, "failed to request Maintenance Mode for storageConsumer")
			return nil, fmt.Errorf("failed to request Maintenance Mode for storageConsumer")
		}
	} else {
		err := s.consumerManager.RemoveAnnotation(ctx, req.StorageConsumerUUID, util.RequestMaintenanceModeAnnotation)
		if err != nil {
			logger.Error(err, "failed to disable Maintenance Mode for storageConsumer")
			return nil, fmt.Errorf("failed to disable Maintenance Mode for storageConsumer")
		}
	}

	logger.Info("Successfully returned from RequestMaintenanceMode")
	return &pb.RequestMaintenanceModeResponse{}, nil
}

func (s *OCSProviderServer) GetStorageClientsInfo(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
	logger := klog.FromContext(ctx).WithName("GetStorageClientsInfo")
	logger.Info("Starting GetStorageClientsInfo RPC", "request", req)

	response := &pb.StorageClientsInfoResponse{}

	var fsid string
	if cephCluster, err := util.GetCephClusterInNamespace(ctx, s.client, s.namespace); err != nil {
		logger.Error(err, "failed to get cephCluster")
		return nil, status.Error(codes.Internal, "failed loading client information")
	} else if cephCluster.Status.CephStatus == nil || cephCluster.Status.CephStatus.FSID == "" {
		logger.Error(fmt.Errorf("waiting for Ceph FSID"), "waiting for Ceph FSID")
		return nil, status.Error(codes.Internal, "failed loading client information")
	} else {
		fsid = cephCluster.Status.CephStatus.FSID
	}

	for i := range req.ClientIDs {
		consumer, err := s.consumerManager.GetByClientID(ctx, req.ClientIDs[i])
		if err != nil {
			logger.Error(err, "failed to get consumer with client id", "client id", req.ClientIDs[i])
			response.Errors = append(response.Errors,
				&pb.StorageClientInfoError{
					ClientID: req.ClientIDs[i],
					Code:     pb.ErrorCode_Internal,
					Message:  "failed loading client information",
				},
			)
		}
		if consumer == nil {
			logger.Info("no consumer found with client id", "client id", req.ClientIDs[i])
			continue
		}

		if !consumer.Spec.Enable {
			logger.Info("consumer is not yet enabled skipping", "client id", req.ClientIDs[i])
			continue
		}

		idx := slices.IndexFunc(consumer.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.Kind == "StorageCluster"
		})
		if idx == -1 {
			logger.Info("consumer is not owned by any storage cluster", "client id", req.ClientIDs[i])
			continue
		}
		owner := &consumer.OwnerReferences[idx]
		if owner.UID != types.UID(req.StorageClusterUID) {
			logger.Info("storageCluster specified on the req does not own the client", "client id", req.ClientIDs[i])
			continue
		}

		consumerConfigMap := &corev1.ConfigMap{}
		consumerConfigMap.Name = consumer.Status.ResourceNameMappingConfigMap.Name
		consumerConfigMap.Namespace = consumer.Namespace
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(consumerConfigMap), consumerConfigMap); err != nil {
			response.Errors = append(response.Errors,
				&pb.StorageClientInfoError{
					ClientID: req.ClientIDs[i],
					Code:     pb.ErrorCode_Internal,
					Message:  "failed loading client information",
				},
			)
			logger.Error(err, "failed to get the consumer configmap")
			continue
		}

		clientInfo := &pb.ClientInfo{ClientID: req.ClientIDs[i], ClientProfiles: map[string]string{}}

		consumerConfig := util.WrapStorageConsumerResourceMap(consumerConfigMap.Data)
		if rns := consumerConfig.GetRbdRadosNamespaceName(); rns != "" {
			clientInfo.RadosNamespace = rns
			clientInfo.RbdStorageID = calculateCephRbdStorageID(fsid, rns)
			clientInfo.ClientProfiles[clientInfoRbdClientProfileKey] = consumerConfig.GetRbdClientProfileName()
		}

		response.ClientsInfo = append(response.ClientsInfo, clientInfo)
	}

	logger.Info("Successfully returned from GetStorageClientsInfo")
	return response, nil
}

func (s *OCSProviderServer) GetBlockPoolsInfo(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
	logger := klog.FromContext(ctx).WithName("GetBlockPoolsInfo")
	logger.Info("Starting GetBlockPoolsInfo RPC", "request", req)

	response := &pb.BlockPoolsInfoResponse{}
	for i := range req.BlockPoolNames {
		cephBlockPool := &rookCephv1.CephBlockPool{}
		cephBlockPool.Name = req.BlockPoolNames[i]
		cephBlockPool.Namespace = s.namespace
		err := s.client.Get(ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
		if kerrors.IsNotFound(err) {
			logger.Info("CephBlockPool not found", "CephBlockPool", cephBlockPool.Name)
			continue
		} else if err != nil {
			logger.Error(err, "failed to get block pool", "CephBlockPool", cephBlockPool.Name)
			response.Errors = append(response.Errors,
				&pb.BlockPoolInfoError{
					BlockPoolName: cephBlockPool.Name,
					Code:          pb.ErrorCode_Internal,
					Message:       "failed loading block pool information",
				},
			)
		}

		var mirroringToken string
		if cephBlockPool.Spec.Mirroring.Enabled &&
			cephBlockPool.Status.Info != nil &&
			cephBlockPool.Status.Info[mirroringTokenKey] != "" {
			secret := &corev1.Secret{}
			secret.Name = cephBlockPool.Status.Info[mirroringTokenKey]
			secret.Namespace = s.namespace
			err := s.client.Get(ctx, client.ObjectKeyFromObject(secret), secret)
			if kerrors.IsNotFound(err) {
				logger.Info(
					"bootstrap secret for CephBlockPool not found",
					"CephBlockPool",
					cephBlockPool.Name,
					"Bootstrap Secret",
					secret.Name,
				)
				continue
			} else if err != nil {
				logger.Error(
					err,
					"failed to get bootstrap secret for CephBlockPool",
					"CephBlockPool",
					cephBlockPool.Name,
					"Bootstrap Secret",
					secret.Name,
				)
				continue
			}
			mirroringToken = string(secret.Data["token"])
		}

		response.BlockPoolsInfo = append(response.BlockPoolsInfo, &pb.BlockPoolInfo{
			BlockPoolName:  cephBlockPool.Name,
			MirroringToken: mirroringToken,
			BlockPoolID:    strconv.Itoa(cephBlockPool.Status.PoolID),
		})

	}
	logger.Info("Successfully returned from GetBlockPoolsInfo")
	return response, nil
}

func (s *OCSProviderServer) isSystemInMaintenanceMode(ctx context.Context) (bool, error) {
	// found - false, not found - true
	cephRBDMirrors := &rookCephv1.CephRBDMirror{}
	cephRBDMirrors.Name = util.CephRBDMirrorName
	cephRBDMirrors.Namespace = s.namespace
	err := s.client.Get(ctx, client.ObjectKeyFromObject(cephRBDMirrors), cephRBDMirrors)
	if client.IgnoreNotFound(err) != nil {
		return false, err
	}
	return kerrors.IsNotFound(err), nil
}

func (s *OCSProviderServer) isConsumerMirrorEnabled(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) (bool, error) {
	clientMappingConfig := &corev1.ConfigMap{}
	clientMappingConfig.Name = util.StorageClientMappingConfigName
	clientMappingConfig.Namespace = s.namespace

	if consumer.Status.Client == nil {
		return false, nil
	}

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(clientMappingConfig), clientMappingConfig); err != nil {
		return false, client.IgnoreNotFound(err)
	}

	return clientMappingConfig.Data[consumer.Status.Client.ID] != "", nil
}

func calculateCephRbdStorageID(cephfsid, radosnamespacename string) string {
	return util.CalculateMD5Hash([2]string{cephfsid, radosnamespacename})
}

func calculateCephFsStorageID(cephfsid, subVolumeGroupName string) string {
	return util.CalculateMD5Hash([2]string{cephfsid, subVolumeGroupName})
}

func (s *OCSProviderServer) getKubeResources(ctx context.Context, logger logr.Logger, consumer *ocsv1alpha1.StorageConsumer) ([]client.Object, error) {

	consumerConfigMap := &v1.ConfigMap{}
	if consumer.Status.ResourceNameMappingConfigMap.Name == "" {
		return nil, fmt.Errorf("waiting for ResourceNameMappingConfig to be generated")
	}
	consumerConfigMap.Name = consumer.Status.ResourceNameMappingConfigMap.Name
	consumerConfigMap.Namespace = consumer.Namespace
	err := s.client.Get(ctx, client.ObjectKeyFromObject(consumerConfigMap), consumerConfigMap)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s configMap. %v", consumerConfigMap.Name, err)
	}
	if consumerConfigMap.Data == nil {
		return nil, fmt.Errorf("waiting StorageConsumer ResourceNameMappingConfig to be generated")
	}
	consumerConfig := util.WrapStorageConsumerResourceMap(consumerConfigMap.Data)

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		return nil, err
	}

	var fsid string
	if cephCluster, err := util.GetCephClusterInNamespace(ctx, s.client, s.namespace); err != nil {
		return nil, err
	} else if cephCluster.Status.CephStatus == nil || cephCluster.Status.CephStatus.FSID == "" {
		return nil, fmt.Errorf("waiting for Ceph FSID")
	} else {
		fsid = cephCluster.Status.CephStatus.FSID
	}

	rbdStorageId := calculateCephRbdStorageID(
		fsid,
		consumerConfig.GetRbdRadosNamespaceName(),
	)
	cephFsStorageId := calculateCephFsStorageID(
		fsid,
		consumerConfig.GetSubVolumeGroupName(),
	)

	if consumer.Status.Client == nil {
		return nil, fmt.Errorf("waiting for the first heart beat before sending the resources")
	}

	mirroringTargetInfo := &pb.ClientInfo{}
	if mirroringTargetInfoInBytes := []byte(consumer.GetAnnotations()[util.StorageConsumerMirroringInfoAnnotation]); len(mirroringTargetInfoInBytes) > 0 {
		if err := json.Unmarshal(mirroringTargetInfoInBytes, mirroringTargetInfo); err != nil {
			return nil, err
		}

	}

	kubeResources := []client.Object{}
	if cephConnection, err := s.getDesiredCephConnection(ctx, consumer, storageCluster); err == nil {
		kubeResources = append(kubeResources, cephConnection)
	} else {
		return nil, err
	}

	kubeResources, err = s.appendClientProfileKubeResources(
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendCephClientSecretKubeResources(
		ctx,
		kubeResources,
		consumer,
		consumerConfig,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendStorageClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
		mirroringTargetInfo.RbdStorageID,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendVolumeSnapshotClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendVolumeGroupSnapshotClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendOdfVolumeGroupSnapshotClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendNetworkFenceClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendVolumeReplicationClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		rbdStorageId,
		mirroringTargetInfo.RbdStorageID,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendVolumeGroupReplicationClassKubeResources(
		ctx,
		logger,
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		mirroringTargetInfo.RbdStorageID,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendClusterResourceQuotaKubeResources(
		kubeResources,
		consumer,
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendClientProfileMappingKubeResources(
		ctx,
		kubeResources,
		consumer,
		consumerConfig,
		mirroringTargetInfo,
	)
	if err != nil {
		return nil, err
	}

	if false {
		kubeResources, err = s.appendNoobaaKubeResources(
			ctx,
			kubeResources,
			consumer,
		)
		if err != nil {
			return nil, err
		}
	}

	return kubeResources, nil
}

func (s *OCSProviderServer) getDesiredCephConnection(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	storageCluster *ocsv1.StorageCluster,
) (*csiopv1.CephConnection, error) {

	configmap := &v1.ConfigMap{}
	configmap.Name = monConfigMap
	configmap.Namespace = consumer.Namespace
	err := s.client.Get(ctx, client.ObjectKeyFromObject(configmap), configmap)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s configMap. %v", monConfigMap, err)
	}
	if configmap.Data["data"] == "" {
		return nil, fmt.Errorf("configmap %s data is empty", monConfigMap)
	}

	monIps, err := extractMonitorIps(configmap.Data["data"])
	if err != nil {
		return nil, fmt.Errorf("failed to extract monitor IPs from configmap %s: %v", monConfigMap, err)
	}

	readAffinityOptions := util.GetReadAffinityOptions(storageCluster)
	readAffinityDisabledAnnotation, _ := strconv.ParseBool(consumer.Annotations["ocs.openshift.io/disable-read-affinity"])

	var readAffinity *csiopv1.ReadAffinitySpec = nil
	if readAffinityOptions.Enabled && !readAffinityDisabledAnnotation {
		labels := readAffinityOptions.CrushLocationLabels
		if len(labels) == 0 {
			labels = []string{
				"kubernetes.io/hostname",
				"topology.kubernetes.io/region",
				"topology.kubernetes.io/zone",
				"topology.rook.io/datacenter",
				"topology.rook.io/room",
				"topology.rook.io/pod",
				"topology.rook.io/pdu",
				"topology.rook.io/row",
				"topology.rook.io/rack",
				"topology.rook.io/chassis",
			}
		}
		readAffinity = &csiopv1.ReadAffinitySpec{
			CrushLocationLabels: labels,
		}
	}

	return &csiopv1.CephConnection{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consumer.Status.Client.Name,
			Namespace: consumer.Status.Client.OperatorNamespace,
		},
		Spec: csiopv1.CephConnectionSpec{
			Monitors:     monIps,
			ReadAffinity: readAffinity,
		},
	}, nil
}

func (s *OCSProviderServer) appendClientProfileKubeResources(
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
) ([]client.Object, error) {
	var kernelMountOptions map[string]string
	for _, option := range strings.Split(util.GetCephFSKernelMountOptions(storageCluster), ",") {
		if kernelMountOptions == nil {
			kernelMountOptions = map[string]string{}
		}
		parts := strings.Split(option, "=")
		kernelMountOptions[parts[0]] = parts[1]
	}

	// The client profile name for all the driver maybe the same or different, hence using a map to merge in case of
	// same name
	profileMap := make(map[string]*csiopv1.ClientProfile)

	rnsName := consumerConfig.GetRbdRadosNamespaceName()
	if rnsName == util.ImplicitRbdRadosNamespaceName {
		rnsName = ""
	}
	rbdClientProfileName := consumerConfig.GetRbdClientProfileName()
	if rbdClientProfileName != "" {
		rbdClientProfile := profileMap[rbdClientProfileName]
		if rbdClientProfile == nil {
			rbdClientProfile = &csiopv1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbdClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[rbdClientProfileName] = rbdClientProfile

		}
		rbdClientProfile.Spec.Rbd = &csiopv1.RbdConfigSpec{
			RadosNamespace: rnsName,
			CephCsiSecrets: &csiopv1.CephCsiSecretsSpec{
				ControllerPublishSecret: corev1.SecretReference{
					Name:      consumerConfig.GetCsiRbdProvisionerCephUserName(),
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
			},
		}
	}

	cephFsClientProfileName := consumerConfig.GetCephFsClientProfileName()
	if cephFsClientProfileName != "" {
		cephFsClientProfile := profileMap[cephFsClientProfileName]
		if cephFsClientProfile == nil {
			cephFsClientProfile = &csiopv1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cephFsClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[cephFsClientProfileName] = cephFsClientProfile
		}
		cephFsClientProfile.Spec.CephFs = &csiopv1.CephFsConfigSpec{
			SubVolumeGroup:     consumerConfig.GetSubVolumeGroupName(),
			KernelMountOptions: kernelMountOptions,
			RadosNamespace:     ptr.To(consumerConfig.GetSubVolumeGroupRadosNamespaceName()),
			CephCsiSecrets: &csiopv1.CephCsiSecretsSpec{
				ControllerPublishSecret: corev1.SecretReference{
					Name:      consumerConfig.GetCsiCephFsProvisionerCephUserName(),
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
			},
		}
	}

	nfsClientProfileName := consumerConfig.GetCephFsClientProfileName()
	if nfsClientProfileName != "" {
		nfsClientProfile := profileMap[nfsClientProfileName]
		if nfsClientProfile == nil {
			nfsClientProfile = &csiopv1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nfsClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[nfsClientProfileName] = nfsClientProfile
		}
		nfsClientProfile.Spec.Nfs = &csiopv1.NfsConfigSpec{}
	}

	for _, profileObj := range profileMap {
		kubeResources = append(kubeResources, profileObj)
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendCephClientSecretKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
) ([]client.Object, error) {

	var err error
	if destSecretName := consumerConfig.GetCsiRbdProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiRbdProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiRbdNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiRbdNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiCephFsProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiCephFsProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiCephFsNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiCephFsNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiNfsProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiNfsProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiNfsNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiNfsNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if kubeResources, err = s.appendCephClientSecretKubeResource(
			ctx,
			kubeResources,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}

	return kubeResources, nil
}

func (s *OCSProviderServer) appendCephClientSecretKubeResource(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	srcSecretName string,
	destSecretName string,
) ([]client.Object, error) {
	cephUserSecret := &v1.Secret{}
	cephUserSecret.Name = srcSecretName
	cephUserSecret.Namespace = consumer.Namespace

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(cephUserSecret), cephUserSecret); err != nil {
		return kubeResources, fmt.Errorf("failed to get %s secret. %v", cephUserSecret, err)
	}

	cephUserSecret.Name = destSecretName
	cephUserSecret.Namespace = consumer.Status.Client.OperatorNamespace
	// clearing the secretType to be empty/Opaque instead of type rook.
	cephUserSecret.Type = ""

	return append(kubeResources, cephUserSecret), nil
}

func (s *OCSProviderServer) appendStorageClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
	remoteRbdStorageId string,
) ([]client.Object, error) {
	scMap := map[string]func() *storagev1.StorageClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		scMap[util.GenerateNameForCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GenerateNameForCephBlockPool(storageCluster.Name),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumerConfig.GetCsiRbdNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				remoteRbdStorageId,
				storageCluster.Spec.ManagedResources.CephBlockPools.DefaultStorageClass,
			)
		}
		scMap[util.GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultVirtRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GenerateNameForCephBlockPool(storageCluster.Name),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumerConfig.GetCsiRbdNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				remoteRbdStorageId,
				storageCluster.Spec.ManagedResources.CephBlockPools.DefaultVirtualizationStorageClass,
			)
		}
		if kmsConfig, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, storageCluster, s.client); err == nil && kmsConfig != nil {
			kmsServiceName := kmsConfig.Data["KMS_SERVICE_NAME"]
			scMap[util.GenerateNameForEncryptedCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
				return util.NewDefaultEncryptedRbdStorageClass(
					consumerConfig.GetRbdClientProfileName(),
					util.GenerateNameForCephBlockPool(storageCluster.Name),
					consumerConfig.GetCsiRbdProvisionerCephUserName(),
					consumerConfig.GetCsiRbdNodeCephUserName(),
					consumer.Status.Client.OperatorNamespace,
					kmsServiceName,
					consumer.GetAnnotations()[defaults.KeyRotationEnableAnnotation],
				)
			}
		}
		scMap[util.GenerateNameForNonResilientCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultNonResilientRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GetTopologyConstrainedPools(storageCluster),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumerConfig.GetCsiRbdNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				remoteRbdStorageId,
			)
		}
	}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		scMap[util.GenerateNameForCephFilesystemStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultCephFsStorageClass(
				consumerConfig.GetCephFsClientProfileName(),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerConfig.GetCsiCephFsProvisionerCephUserName(),
				consumerConfig.GetCsiCephFsNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				cephFsStorageId,
			)
		}
	}
	if consumerConfig.GetNfsClientProfileName() != "" {
		scMap[util.GenerateNameForCephNetworkFilesystemStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultNFSStorageClass(
				consumerConfig.GetNfsClientProfileName(),
				util.GenerateNameForCephNFS(storageCluster),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				util.GenerateNameForNFSService(storageCluster),
				consumerConfig.GetCsiNfsProvisionerCephUserName(),
				consumerConfig.GetCsiNfsNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
			)
		}
	}

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.StorageClasses,
		"StorageClass",
		func(scName string) (client.Object, error) {
			if scGen, fnExist := scMap[scName]; fnExist {
				return scGen(), nil
			} else {
				return util.StorageClassFromExisting(
					ctx,
					s.client,
					scName,
					consumer,
					consumerConfig,
					rbdStorageId,
					cephFsStorageId,
					cephFsStorageId,
					remoteRbdStorageId,
				)
			}
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]client.Object, error) {
	vscMap := map[string]func() *snapapi.VolumeSnapshotClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		vscMap[util.GenerateNameForSnapshotClass(storageCluster.Name, util.RbdSnapshotter)] = func() *snapapi.VolumeSnapshotClass {
			return util.NewDefaultRbdSnapshotClass(
				consumerConfig.GetRbdClientProfileName(),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
			)
		}
	}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		vscMap[util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter)] = func() *snapapi.VolumeSnapshotClass {
			return util.NewDefaultCephFsSnapshotClass(
				consumerConfig.GetCephFsClientProfileName(),
				consumerConfig.GetCsiCephFsProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				cephFsStorageId,
			)
		}
	}
	if consumerConfig.GetNfsClientProfileName() != "" {
		vscMap[util.GenerateNameForSnapshotClass(storageCluster.Name, util.NfsSnapshotter)] = func() *snapapi.VolumeSnapshotClass {
			return util.NewDefaultNfsSnapshotClass(
				consumerConfig.GetNfsClientProfileName(),
				consumerConfig.GetCsiNfsProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
			)
		}
	}

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.VolumeSnapshotClasses,
		"VolumeSnapshotClass",
		func(vscName string) (client.Object, error) {
			if vscGen, fnExist := vscMap[vscName]; fnExist {
				return vscGen(), nil
			} else {
				return util.VolumeSnapshotClassFromExisting(
					ctx,
					s.client,
					vscName,
					consumer,
					consumerConfig,
					rbdStorageId,
					cephFsStorageId,
					cephFsStorageId,
				)
			}
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeGroupSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]client.Object, error) {
	vgscMap := map[string]func() *groupsnapapi.VolumeGroupSnapshotClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		vgscMap[util.GenerateNameForGroupSnapshotClass(storageCluster, util.RbdGroupSnapshotter)] = func() *groupsnapapi.VolumeGroupSnapshotClass {
			return util.NewDefaultRbdGroupSnapshotClass(
				consumerConfig.GetRbdClientProfileName(),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				util.GenerateNameForCephBlockPool(storageCluster.Name),
				rbdStorageId,
			)
		}
	}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		vgscMap[util.GenerateNameForGroupSnapshotClass(storageCluster, util.CephfsGroupSnapshotter)] = func() *groupsnapapi.VolumeGroupSnapshotClass {
			return util.NewDefaultCephFsGroupSnapshotClass(
				consumerConfig.GetCephFsClientProfileName(),
				consumerConfig.GetCsiCephFsProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				cephFsStorageId,
			)
		}
	}

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.VolumeGroupSnapshotClasses,
		"volumegroupsnapshotclass.groupsnapshot.storage.k8s.io",
		func(vgscName string) (client.Object, error) {
			if vgscGen, fnExist := vgscMap[vgscName]; fnExist {
				return vgscGen(), nil
			} else {
				return util.VolumeGroupSnapshotClassFromExisting(
					ctx,
					s.client,
					vgscName,
					consumer,
					consumerConfig,
					rbdStorageId,
					cephFsStorageId,
					cephFsStorageId,
				)
			}
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendOdfVolumeGroupSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	cephFsStorageId string,
) ([]client.Object, error) {
	vgscMap := map[string]func() *odfgsapiv1b1.VolumeGroupSnapshotClass{}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		vgscMap[util.GenerateNameForGroupSnapshotClass(storageCluster, util.CephfsGroupSnapshotter)] = func() *odfgsapiv1b1.VolumeGroupSnapshotClass {
			return util.NewDefaultOdfCephFsGroupSnapshotClass(
				consumerConfig.GetCephFsClientProfileName(),
				consumerConfig.GetCsiCephFsProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				cephFsStorageId,
			)
		}
	}

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.VolumeGroupSnapshotClasses,
		"volumegroupsnapshotclass.groupsnapshot.storage.openshift.io",
		func(vgscName string) (client.Object, error) {
			if vgscGen, fnExist := vgscMap[vgscName]; fnExist {
				return vgscGen(), nil
			} else {
				return util.OdfVolumeGroupSnapshotClassFromExisting(
					ctx,
					s.client,
					vgscName,
					consumer,
					consumerConfig,
					cephFsStorageId,
				)
			}
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendNetworkFenceClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]client.Object, error) {
	nfcMap := map[string]func() *csiaddonsv1alpha1.NetworkFenceClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		nfcMap[util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)] = func() *csiaddonsv1alpha1.NetworkFenceClass {
			return util.NewDefaultRbdNetworkFenceClass(
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
			)
		}
	}

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.NetworkFenceClasses,
		"networkfenceclass",
		func(nfcName string) (client.Object, error) {
			if nfcGen, fnExist := nfcMap[nfcName]; fnExist {
				return nfcGen(), nil
			} else {
				return nil, nil
			}
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeReplicationClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	rbdStorageId string,
	remoteRbdStorageId string,
) ([]client.Object, error) {

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.VolumeReplicationClasses,
		"VolumeReplicationClass",
		func(vrcName string) (client.Object, error) {
			return util.VolumeReplicationClassFromTemplate(
				ctx,
				s.client,
				vrcName,
				consumer,
				consumerConfig,
				rbdStorageId,
				remoteRbdStorageId,
			)
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeGroupReplicationClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId string,
	remoteRbdStorageId string,
) ([]client.Object, error) {

	resources := getKubeResourcesForClass(
		logger,
		consumer.Spec.VolumeGroupReplicationClasses,
		"VolumeGroupReplicationClass",
		func(vgrcName string) (client.Object, error) {
			return util.VolumeGroupReplicationClassFromTemplate(
				ctx,
				s.client,
				vgrcName,
				consumer,
				consumerConfig,
				rbdStorageId,
				remoteRbdStorageId,
			)
		},
	)
	kubeResources = append(kubeResources, resources...)

	return kubeResources, nil
}

func (s *OCSProviderServer) appendClusterResourceQuotaKubeResources(
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]client.Object, error) {
	if consumer.Spec.StorageQuotaInGiB > 0 {
		clusterResourceQuota := &quotav1.ClusterResourceQuota{
			ObjectMeta: metav1.ObjectMeta{
				Name: util.GetClusterResourceQuotaName(consumer.Status.Client.Name),
			},
			Spec: quotav1.ClusterResourceQuotaSpec{
				Selector: quotav1.ClusterResourceQuotaSelector{
					LabelSelector: &metav1.LabelSelector{
						MatchExpressions: []metav1.LabelSelectorRequirement{
							{
								Key:      string(consumer.UID),
								Operator: metav1.LabelSelectorOpDoesNotExist,
							},
						},
					},
				},
				Quota: corev1.ResourceQuotaSpec{
					Hard: corev1.ResourceList{"requests.storage": *resource.NewQuantity(
						int64(consumer.Spec.StorageQuotaInGiB)*oneGibInBytes,
						resource.BinarySI,
					)},
				},
			},
		}

		kubeResources = append(kubeResources, clusterResourceQuota)
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendClientProfileMappingKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	mirroringTargetInfo *pb.ClientInfo,
) ([]client.Object, error) {
	cbpList := &rookCephv1.CephBlockPoolList{}
	if err := s.client.List(ctx, cbpList, client.InNamespace(s.namespace)); err != nil {
		return kubeResources, fmt.Errorf("failed to list cephBlockPools in namespace. %v", err)
	}
	blockPoolMapping := []csiopv1.BlockPoolIdPair{}
	for i := range cbpList.Items {
		cephBlockPool := &cbpList.Items[i]
		remoteBlockPoolID := cephBlockPool.GetAnnotations()[util.BlockPoolMirroringTargetIDAnnotation]
		if remoteBlockPoolID != "" {
			localBlockPoolID := strconv.Itoa(cephBlockPool.Status.PoolID)
			blockPoolMapping = append(
				blockPoolMapping,
				csiopv1.BlockPoolIdPair{localBlockPoolID, remoteBlockPoolID},
			)
		}
	}

	remoteClientProfileName := mirroringTargetInfo.ClientProfiles[clientInfoRbdClientProfileKey]
	if len(blockPoolMapping) > 0 && remoteClientProfileName != "" {
		kubeResources = append(
			kubeResources,
			&csiopv1.ClientProfileMapping{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumer.Status.Client.Name,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1.ClientProfileMappingSpec{
					Mappings: []csiopv1.MappingsSpec{
						{
							LocalClientProfile:  consumerConfig.GetRbdClientProfileName(),
							RemoteClientProfile: remoteClientProfileName,
							BlockPoolIdMapping:  blockPoolMapping,
						},
					},
				},
			},
		)
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendNoobaaKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]client.Object, error) {
	// Noobaa Configuration
	// Fetch noobaa remote secret and management address and append to extResources
	noobaaOperatorSecret := &v1.Secret{}
	noobaaOperatorSecret.Name = fmt.Sprintf("noobaa-account-%s", consumer.Name)
	noobaaOperatorSecret.Namespace = s.namespace

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(noobaaOperatorSecret), noobaaOperatorSecret); kerrors.IsNotFound(err) {
		return kubeResources, nil
	} else if err != nil {
		return kubeResources, fmt.Errorf("failed to get %s secret. %v", noobaaOperatorSecret.Name, err)
	}

	authToken, ok := noobaaOperatorSecret.Data["auth_token"]
	if !ok || len(authToken) == 0 {
		return kubeResources, fmt.Errorf("auth_token not found in %s secret", noobaaOperatorSecret.Name)
	}

	noobaMgmtRoute := &routev1.Route{}
	noobaMgmtRoute.Name = "noobaa-mgmt"
	noobaMgmtRoute.Namespace = s.namespace

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(noobaMgmtRoute), noobaMgmtRoute); err != nil {
		return kubeResources, fmt.Errorf("failed to get noobaa-mgmt route. %v", err)
	}
	if len(noobaMgmtRoute.Status.Ingress) == 0 {
		return kubeResources, fmt.Errorf("no Ingress available in noobaa-mgmt route")
	}

	noobaaMgmtAddress := noobaMgmtRoute.Status.Ingress[0].Host
	if noobaaMgmtAddress == "" {
		return kubeResources, fmt.Errorf("no Host found in noobaa-mgmt route Ingress")
	}

	kubeResources = append(kubeResources,
		&corev1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "noobaa-remote-join-secret",
				Namespace: consumer.Status.Client.OperatorNamespace,
			},
			Data: map[string][]byte{
				"auth_token": authToken,
				"mgmt_addr":  []byte(noobaaMgmtAddress),
			},
		},
		&nbv1.NooBaa{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "noobaa-remote",
				Namespace: consumer.Status.Client.OperatorNamespace,
			},
			Spec: nbv1.NooBaaSpec{
				JoinSecret: &v1.SecretReference{
					Name:      "noobaa-remote-join-secret",
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
			},
		},
	)

	return kubeResources, nil
}

func sanitizeKubeResource(obj client.Object) {
	name := obj.GetName()
	namespace := obj.GetNamespace()
	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	zeroFieldByName(obj, "Status")
	zeroFieldByName(obj, "ObjectMeta")

	obj.SetName(name)
	obj.SetNamespace(namespace)
	obj.SetLabels(labels)
	obj.SetAnnotations(annotations)
}

func zeroFieldByName(obj any, fieldName string) {
	st := reflect.Indirect(reflect.ValueOf(obj))
	if st.Kind() != reflect.Struct {
		return
	}

	field := st.FieldByName(fieldName)
	if field.CanSet() {
		field.SetZero()
	}
}

func getKubeResourcesForClass[T CommonClassSpecAccessors](
	logger logr.Logger,
	classList []T,
	classDisplayName string,
	genClassKubeObjFn func(string) (client.Object, error),
) []client.Object {
	classNameMapping := map[string]string{}
	for i := len(classList) - 1; i >= 0; i-- {
		src := classList[i].GetName()
		for _, alias := range classList[i].GetAliases() {
			classNameMapping[alias] = src
		}
	}

	for i := len(classList) - 1; i >= 0; i-- {
		clsSpec := classList[i]
		classNameMapping[cmp.Or(clsSpec.GetRename(), clsSpec.GetName())] = clsSpec.GetName()
	}

	kubeResources := []client.Object{}
	srcClassCache := map[string]client.Object{}
	for destName, srcName := range classNameMapping {
		var srcKubeObj client.Object
		if srcKubeObj = srcClassCache[srcName]; srcKubeObj == nil {
			var err error
			srcKubeObj, err = genClassKubeObjFn(srcName)
			if kerrors.IsNotFound(err) {
				logger.Info("Resource with name doesn't exist in the cluster", "Resource", classDisplayName, "Name", srcName)
			} else if errors.Is(err, util.UnsupportedProvisioner) {
				logger.Info("Encountered unsupported provisioner", "Resource", classDisplayName, "Name", srcName)
			} else if errors.Is(err, util.UnsupportedDriver) {
				logger.Info("Encountered unsupported driver", "Resource", classDisplayName, "Name", srcName)
			} else if reflect.ValueOf(srcKubeObj).IsNil() {
				logger.Info("Resource name does not points to a builtin or an existing object, skipping", "Resource", classDisplayName, "Name", srcName)
			} else if srcKubeObj.GetLabels()[util.ExternalClassLabelKey] == "true" {
				logger.Info("Resource is an external object, skipping", "Resource", classDisplayName, "Name", srcName)
			} else {
				srcClassCache[srcName] = srcKubeObj
			}
		}
		if _, exist := srcClassCache[srcName]; exist {
			distKubeObj := srcKubeObj.DeepCopyObject().(client.Object)
			distKubeObj.SetName(destName)
			kubeResources = append(kubeResources, distKubeObj)
		}
	}
	return kubeResources
}

func checkClientPreConditions(consumer *ocsv1alpha1.StorageConsumer, ocsOpVersion string, logger logr.Logger) bool {
	if consumer == nil || consumer.Status.Client == nil {
		logger.Error(fmt.Errorf("failed precondition"), "client status is not available")
		return false
	}

	clientOpVersion := consumer.Status.Client.OperatorVersion
	if clientOpVersion == notAvailable {
		logger.Error(fmt.Errorf("failed precondition"), "client version in client status is not available")
		return false
	}

	ocsOpSemver := semver.MustParse(ocsOpVersion)
	clientOpSemver := semver.MustParse(clientOpVersion)
	if ocsOpSemver.Major < clientOpSemver.Major || ocsOpSemver.Minor < clientOpSemver.Minor {
		logger.Error(fmt.Errorf("failed precondition"), "client version is ahead of server version")
		return false
	}
	return true
}
