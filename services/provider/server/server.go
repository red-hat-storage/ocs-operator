package server

import (
	"cmp"
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/mirroring"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	ocsVersion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	csiopv1a1 "github.com/ceph/ceph-csi-operator/api/v1alpha1"
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	nbapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	templatev1 "github.com/openshift/api/template/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
)

const (
	ProviderCertsMountPoint   = "/mnt/cert"
	onboardingTicketKeySecret = "onboarding-ticket-key"
	storageRequestNameLabel   = "ocs.openshift.io/storagerequest-name"
	notAvailable              = "N/A"

	ramenDRStorageIDLabelKey         = "ramendr.openshift.io/storageid"
	ramenDRReplicationIDLabelKey     = "ramendr.openshift.io/replicationid"
	ramenDRFlattenModeLabelKey       = "replication.storage.openshift.io/flatten-mode"
	ramenMaintenanceModeLabelKey     = "ramendr.openshift.io/maintenancemodes"
	oneGibInBytes                    = 1024 * 1024 * 1024
	monConfigMap                     = "rook-ceph-mon-endpoints"
	monSecret                        = "rook-ceph-mon"
	volumeReplicationClass5mSchedule = "5m"
	mirroringTokenKey                = "rbdMirrorBootstrapPeerSecretName"
)

type OCSProviderServer struct {
	pb.UnimplementedOCSProviderServer
	client                    client.Client
	scheme                    *runtime.Scheme
	consumerManager           *ocsConsumerManager
	storageRequestManager     *storageRequestManager
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

	storageRequestManager, err := newStorageRequestManager(client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new StorageRequest instance. %v", err)
	}

	storageClusterPeerManager, err := newStorageClusterPeerManager(client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new StorageClusterPeer instance. %v", err)
	}

	return &OCSProviderServer{
		client:                    client,
		scheme:                    scheme,
		consumerManager:           consumerManager,
		storageRequestManager:     storageRequestManager,
		storageClusterPeerManager: storageClusterPeerManager,
		namespace:                 namespace,
	}, nil
}

// OnboardConsumer RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) OnboardConsumer(ctx context.Context, req *pb.OnboardConsumerRequest) (*pb.OnboardConsumerResponse, error) {

	version, err := semver.FinalizeVersion(req.ClientOperatorVersion)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "malformed ClientOperatorVersion for client %q is provided. %v", req.ConsumerName, err)
	}

	serverVersion, _ := semver.Make(ocsVersion.Version)
	clientVersion, _ := semver.Make(version)
	if serverVersion.Major != clientVersion.Major || serverVersion.Minor != clientVersion.Minor {
		return nil, status.Errorf(codes.FailedPrecondition, "both server and client %q operators major and minor versions should match for onboarding process", req.ConsumerName)
	}

	pubKey, err := s.getOnboardingValidationKey(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get public key to validate onboarding ticket for consumer %q. %v", req.ConsumerName, err)
	}

	onboardingTicket, err := decodeAndValidateTicket(req.OnboardingTicket, pubKey)
	if err != nil {
		klog.Errorf("failed to validate onboarding ticket for consumer %q. %v", req.ConsumerName, err)
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket is not valid. %v", err)
	}

	if onboardingTicket.SubjectRole != services.ClientRole {
		err := fmt.Errorf("invalid onboarding ticket for %q, expecting role %s found role %s", req.ConsumerName, services.ClientRole, onboardingTicket.SubjectRole)
		klog.Error(err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	storageConsumer := &ocsv1alpha1.StorageConsumer{}
	storageConsumer.Name = onboardingTicket.ID
	storageConsumer.Namespace = s.namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); err != nil {
		klog.Errorf("failed to get storageconsumer referred by the supplied token: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get storageconsumer. %v", err)
	} else if storageConsumer.Spec.Enable {
		klog.Errorf("storageconsumer is already enabled %s", storageConsumer.Name)
		return nil, status.Errorf(codes.InvalidArgument, "refusing to onboard onto storageconsumer with supplied token")
	}

	onboardingSecret := &corev1.Secret{}
	onboardingSecret.Name = fmt.Sprintf("onboarding-token-%s", storageConsumer.UID)
	onboardingSecret.Namespace = s.namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(onboardingSecret), onboardingSecret); err != nil {
		klog.Errorf("failed to get onboarding secret corresponding to storageconsumer %s: %v", storageConsumer.Name, err)
		return nil, status.Errorf(codes.Internal, "failed to get onboarding secret. %v", err)
	}
	if req.OnboardingTicket != string(onboardingSecret.Data[defaults.OnboardingTokenKey]) {
		klog.Errorf("supplied onboarding ticket does not match storageconsumer secret")
		return nil, status.Errorf(codes.InvalidArgument, "supplied onboarding ticket does not match mapped secret")
	}

	storageConsumerUUID, err := s.consumerManager.EnableStorageConsumer(ctx, storageConsumer)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to onboard on storageConsumer resource. %v", err)
	}
	return &pb.OnboardConsumerResponse{StorageConsumerUUID: storageConsumerUUID}, nil
}

// AcknowledgeOnboarding acknowledge the onboarding is complete
func (s *OCSProviderServer) AcknowledgeOnboarding(ctx context.Context, req *pb.AcknowledgeOnboardingRequest) (*pb.AcknowledgeOnboardingResponse, error) {
	return nil, status.Errorf(codes.Unimplemented, "Not expecting a two step onboarding process")
}

// GetStorageConfig RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) GetStorageConfig(ctx context.Context, req *pb.StorageConfigRequest) (*pb.StorageConfigResponse, error) {

	// Get storage consumer resource using UUID
	consumerObj, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, err
	}

	klog.Infof("Found storageConsumer for GetStorageConfig")

	// Verify Status
	switch consumerObj.Status.State {
	case ocsv1alpha1.StorageConsumerStateNotEnabled:
		return nil, status.Errorf(codes.FailedPrecondition, "storageConsumer is in not enabled")
	case ocsv1alpha1.StorageConsumerStateFailed:
		// TODO: get correct error message from the storageConsumer status
		return nil, status.Errorf(codes.Internal, "storageConsumer status failed")
	case ocsv1alpha1.StorageConsumerStateConfiguring:
		return nil, status.Errorf(codes.Unavailable, "waiting for the rook resources to be provisioned")
	case ocsv1alpha1.StorageConsumerStateDeleting:
		return nil, status.Errorf(codes.NotFound, "storageConsumer is already in deleting phase")
	case ocsv1alpha1.StorageConsumerStateReady:
		kubeResources, err := s.getKubeResources(ctx, consumerObj)
		if err != nil {
			klog.Errorf("failed to get kube resources: %v", err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		response := &pb.StorageConfigResponse{SystemAttributes: &pb.SystemAttributes{}}

		for _, kubeResource := range kubeResources {
			gvk, err := apiutil.GVKForObject(kubeResource, s.scheme)
			if err != nil {
				return nil, err
			}
			switch gvk {
			case csiopv1a1.GroupVersion.WithKind("CephConnection"):
				cephConn := kubeResource.(*csiopv1a1.CephConnection)
				response.ExternalResource = append(
					response.ExternalResource,
					&pb.ExternalResource{
						Name: cephConn.Name,
						Kind: "CephConnection",
						Data: mustMarshal(cephConn.Spec),
					},
				)
			case quotav1.SchemeGroupVersion.WithKind("ClusterResourceQuota"):
				quota := kubeResource.(*quotav1.ClusterResourceQuota)
				response.ExternalResource = append(
					response.ExternalResource,
					&pb.ExternalResource{
						Name: quota.Name,
						Kind: "ClusterResourceQuota",
						Data: mustMarshal(quota.Spec),
					},
				)
			case csiopv1a1.GroupVersion.WithKind("ClientProfileMapping"):
				clientProfileMapping := kubeResource.(*csiopv1a1.ClientProfileMapping)
				response.ExternalResource = append(
					response.ExternalResource,
					&pb.ExternalResource{
						Name: clientProfileMapping.Name,
						Kind: "ClientProfileMapping",
						Data: mustMarshal(clientProfileMapping.Spec),
					},
				)
			case nbv1.SchemeGroupVersion.WithKind("Noobaa"):
				noobaa := kubeResource.(*nbv1.NooBaa)
				response.ExternalResource = append(
					response.ExternalResource,
					&pb.ExternalResource{
						Name: noobaa.Name,
						Kind: "Noobaa",
						Data: mustMarshal(noobaa.Spec),
					},
				)
			case corev1.SchemeGroupVersion.WithKind("Secret"):
				secret := kubeResource.(*corev1.Secret)
				if secret.Name == "noobaa-remote-join-secret" {
					oldSecretFormat := map[string]string{}
					for k, v := range secret.Data {
						oldSecretFormat[k] = string(v)
					}
					response.ExternalResource = append(
						response.ExternalResource,
						&pb.ExternalResource{
							Name: secret.Name,
							Kind: "Secret",
							Data: mustMarshal(oldSecretFormat),
						},
					)
				}
			}
		}

		inMaintenanceMode, err := s.isSystemInMaintenanceMode(ctx)
		if err != nil {
			klog.Error(err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.SystemAttributes.SystemInMaintenanceMode = inMaintenanceMode

		isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, consumerObj)
		if err != nil {
			klog.Error(err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.SystemAttributes.MirrorEnabled = isConsumerMirrorEnabled

		channelName, err := s.getOCSSubscriptionChannel(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
		if err != nil {
			return nil, err
		}

		desiredClientConfigHash := getDesiredClientConfigHash(
			channelName,
			consumerObj,
			isEncryptionInTransitEnabled(storageCluster.Spec.Network),
			inMaintenanceMode,
			isConsumerMirrorEnabled,
		)

		response.DesiredConfigHash = desiredClientConfigHash

		klog.Infof("successfully returned the config details to the consumer.")
		return response, nil
	}

	return nil, status.Errorf(codes.Unavailable, "storage consumer status is not set")
}

// GetDesiredClientState RPC call to generate the desired state of the client
func (s *OCSProviderServer) GetDesiredClientState(ctx context.Context, req *pb.GetDesiredClientStateRequest) (*pb.GetDesiredClientStateResponse, error) {

	// Get storage consumer resource using UUID
	consumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		klog.Errorf("failed to get StorageConsumer: %v", err)
		return nil, status.Errorf(codes.Internal, "failed to get StorageConsumer")
	}

	klog.Infof("Found StorageConsumer for GetDesiredClientState")

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
		kubeResources, err := s.getKubeResources(ctx, consumer)
		if err != nil {
			klog.Errorf("failed to get kube resources: %v", err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		response := &pb.GetDesiredClientStateResponse{}

		for _, kubeResource := range kubeResources {
			gvk, err := apiutil.GVKForObject(kubeResource, s.scheme)
			if err != nil {
				return nil, err
			}
			if kubeResource.GetName() == "" {
				klog.Errorf("Resource is missing a name: %v", kubeResource)
				return nil, status.Errorf(codes.Internal, "failed to produce client state.")
			}
			kubeResource.GetObjectKind().SetGroupVersionKind(gvk)
			sanitizeKubeResource(kubeResource)
			response.KubeResources = append(response.KubeResources, mustMarshal(kubeResource))
		}

		channelName, err := s.getOCSSubscriptionChannel(ctx)
		if err != nil {
			klog.Errorf("failed to get channel name for Client Operator: %v", err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.ClientOperatorChannel = channelName

		inMaintenanceMode, err := s.isSystemInMaintenanceMode(ctx)
		if err != nil {
			klog.Error(err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.MaintenanceMode = inMaintenanceMode

		isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, consumer)
		if err != nil {
			klog.Error(err)
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		response.MirrorEnabled = isConsumerMirrorEnabled

		storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
		if err != nil {
			return nil, err
		}

		desiredClientConfigHash := getDesiredClientConfigHash(
			channelName,
			consumer,
			isEncryptionInTransitEnabled(storageCluster.Spec.Network),
			inMaintenanceMode,
			isConsumerMirrorEnabled,
		)
		response.DesiredStateHash = desiredClientConfigHash

		klog.Infof("successfully returned the config details to the consumer.")
		return response, nil
	}

	return nil, status.Errorf(codes.Unavailable, "StorageConsumer status is not set")

}

// OffboardConsumer RPC call to delete the StorageConsumer CR
func (s *OCSProviderServer) OffboardConsumer(ctx context.Context, req *pb.OffboardConsumerRequest) (*pb.OffboardConsumerResponse, error) {
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
	if err = csiopv1a1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add csiopv1a1 to scheme. %v", err)
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
	if err = replicationv1alpha1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add replicationv1alpha1 to scheme. %v", err)
	}
	if err = quotav1.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add quotav1 to scheme. %v", err)
	}
	if err = nbapis.AddToScheme(scheme); err != nil {
		return nil, fmt.Errorf("failed to add nbapis to scheme. %v", err)
	}

	return scheme, nil
}

func (s *OCSProviderServer) getCephClientInformation(ctx context.Context, name string) (string, string, error) {
	cephClient := &rookCephv1.CephClient{}
	err := s.client.Get(ctx, types.NamespacedName{Name: name, Namespace: s.namespace}, cephClient)
	if err != nil {
		return "", "", fmt.Errorf("failed to get rook ceph client %s secret. %v", name, err)
	}
	if cephClient.Status == nil {
		return "", "", fmt.Errorf("rook ceph client %s status is nil", name)
	}
	if cephClient.Status.Info == nil {
		return "", "", fmt.Errorf("rook ceph client %s Status.Info is empty", name)
	}

	if len(cephClient.Annotations) == 0 {
		return "", "", fmt.Errorf("rook ceph client %s annotation is empty", name)
	}
	if cephClient.Annotations[controllers.StorageRequestAnnotation] == "" || cephClient.Annotations[controllers.StorageCephUserTypeAnnotation] == "" {
		klog.Warningf("rook ceph client %s has missing storage annotations", name)
	}

	return cephClient.Status.Info["secretName"], cephClient.Annotations[controllers.StorageCephUserTypeAnnotation], nil
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

func mustMarshal[T any](value T) []byte {
	newData, err := json.Marshal(value)
	if err != nil {
		panic("failed to marshal")
	}
	return newData
}

func getSubVolumeGroupClusterID(subVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup) string {
	str := fmt.Sprintf(
		"%s-%s-file-%s",
		subVolumeGroup.Namespace,
		subVolumeGroup.Spec.FilesystemName,
		subVolumeGroup.Name,
	)
	hash := sha256.Sum256([]byte(str))
	return hex.EncodeToString(hash[:16])
}

func decodeAndValidateTicket(ticket string, pubKey *rsa.PublicKey) (*services.OnboardingTicket, error) {
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

	if ticketData.ExpirationDate < time.Now().Unix() {
		return nil, fmt.Errorf("onboarding ticket %s is expired", ticketData.ID)
	}

	klog.Infof("onboarding ticket %s has been verified successfully", ticketData.ID)

	return &ticketData, nil
}

// FulfillStorageClaim RPC call to create the StorageClaim CR on
// provider cluster.
func (s *OCSProviderServer) FulfillStorageClaim(ctx context.Context, req *pb.FulfillStorageClaimRequest) (*pb.FulfillStorageClaimResponse, error) {
	// Get storage consumer resource using UUID
	consumerObj, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, status.Error(codes.Internal, err.Error())
	}

	klog.Infof("Found StorageConsumer %q (%q)", consumerObj.Name, req.StorageConsumerUUID)

	var storageType string
	switch req.StorageType {
	case pb.FulfillStorageClaimRequest_BLOCK:
		storageType = "block"
	case pb.FulfillStorageClaimRequest_SHAREDFILE:
		storageType = "sharedfile"
	default:
		return nil, status.Errorf(codes.InvalidArgument, "encountered an unknown storage type, %s", storageType)
	}

	err = s.storageRequestManager.Create(ctx, consumerObj, req.StorageClaimName, storageType, req.EncryptionMethod, req.StorageProfile)
	if err != nil {
		errMsg := fmt.Sprintf("failed to fulfill storage class claim for %q. %v", req.StorageConsumerUUID, err)
		klog.Error(errMsg)
		if kerrors.IsAlreadyExists(err) {
			return nil, status.Error(codes.AlreadyExists, errMsg)
		}
		return nil, status.Error(codes.Internal, errMsg)
	}

	return &pb.FulfillStorageClaimResponse{}, nil
}

// RevokeStorageClaim RPC call to delete the StorageClaim CR on
// provider cluster.
func (s *OCSProviderServer) RevokeStorageClaim(ctx context.Context, req *pb.RevokeStorageClaimRequest) (*pb.RevokeStorageClaimResponse, error) {
	err := s.storageRequestManager.Delete(ctx, req.StorageConsumerUUID, req.StorageClaimName)
	if err != nil {
		errMsg := fmt.Sprintf("failed to revoke storage class claim %q for %q. %v", req.StorageClaimName, req.StorageConsumerUUID, err)
		klog.Error(errMsg)
		return nil, status.Error(codes.Internal, errMsg)
	}

	return &pb.RevokeStorageClaimResponse{}, nil
}

func storageClaimCephCsiSecretName(secretType, suffix string) string {
	return fmt.Sprintf("ceph-client-%s-%s", secretType, suffix)
}

// GetStorageClaim RPC call to get the ceph resources for the StorageClaim.
func (s *OCSProviderServer) GetStorageClaimConfig(ctx context.Context, req *pb.StorageClaimConfigRequest) (*pb.StorageClaimConfigResponse, error) {
	storageRequest, err := s.storageRequestManager.Get(ctx, req.StorageConsumerUUID, req.StorageClaimName)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get storage class claim config %q for %q. %v", req.StorageClaimName, req.StorageConsumerUUID, err)
		if kerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, errMsg)
		}
		return nil, status.Error(codes.Internal, errMsg)
	}

	// Verify Status.Phase
	msg := fmt.Sprintf("storage claim %q for %q is in %q phase", req.StorageClaimName, req.StorageConsumerUUID, storageRequest.Status.Phase)
	klog.Info(msg)
	if storageRequest.Status.Phase != ocsv1alpha1.StorageRequestReady {
		switch storageRequest.Status.Phase {
		case ocsv1alpha1.StorageRequestFailed:
			return nil, status.Error(codes.Internal, msg)
		case ocsv1alpha1.StorageRequestInitializing:
			return nil, status.Error(codes.Unavailable, msg)
		case ocsv1alpha1.StorageRequestCreating:
			return nil, status.Error(codes.Unavailable, msg)
		case "":
			return nil, status.Errorf(codes.Unavailable, "status is not set for storage class claim %q for %q", req.StorageClaimName, req.StorageConsumerUUID)
		default:
			return nil, status.Error(codes.Internal, msg)
		}
	}

	clientProfilemd5Sum := md5.Sum([]byte(req.StorageClaimName))
	clientProfile := hex.EncodeToString(clientProfilemd5Sum[:])

	var extR []*pb.ExternalResource

	storageRequestHash := getStorageRequestHash(req.StorageConsumerUUID, req.StorageClaimName)

	cephCluster, err := util.GetCephClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		return nil, err
	}

	for _, cephRes := range storageRequest.Status.CephResources {
		switch cephRes.Kind {
		case "CephClient":
			clientSecretName, clientUserType, err := s.getCephClientInformation(ctx, cephRes.Name)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			extSecretName := storageClaimCephCsiSecretName(clientUserType, storageRequestHash)

			cephUserSecret := &v1.Secret{}
			err = s.client.Get(ctx, types.NamespacedName{Name: clientSecretName, Namespace: s.namespace}, cephUserSecret)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s secret. %v", clientSecretName, err)
			}

			idProp := "userID"
			keyProp := "userKey"
			if storageRequest.Spec.Type == "sharedfile" {
				idProp = "adminID"
				keyProp = "adminKey"
			}
			extR = append(extR, &pb.ExternalResource{
				Name: extSecretName,
				Kind: "Secret",
				Data: mustMarshal(map[string]string{
					idProp:  cephRes.Name,
					keyProp: string(cephUserSecret.Data[cephRes.Name]),
				}),
			})

		case "CephBlockPoolRadosNamespace":
			rns := &rookCephv1.CephBlockPoolRadosNamespace{}
			err = s.client.Get(ctx, types.NamespacedName{Name: cephRes.Name, Namespace: s.namespace}, rns)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s CephBlockPoolRadosNamespace. %v", cephRes.Name, err)
			}
			provisionerSecretName := storageClaimCephCsiSecretName("provisioner", storageRequestHash)
			nodeSecretName := storageClaimCephCsiSecretName("node", storageRequestHash)
			rbdStorageClassData := map[string]string{
				"clusterID":                 s.namespace,
				"radosnamespace":            cephRes.Name,
				"pool":                      rns.Spec.BlockPoolName,
				"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
				"csi.storage.k8s.io/fstype": "ext4",
				"imageFormat":               "2",
				"csi.storage.k8s.io/provisioner-secret-name":       provisionerSecretName,
				"csi.storage.k8s.io/node-stage-secret-name":        nodeSecretName,
				"csi.storage.k8s.io/controller-expand-secret-name": provisionerSecretName,
			}
			if storageRequest.Spec.EncryptionMethod != "" {
				rbdStorageClassData["encrypted"] = "true"
				rbdStorageClassData["encryptionKMSID"] = storageRequest.Spec.EncryptionMethod
			}

			blockPool := &rookCephv1.CephBlockPool{}
			err = s.client.Get(ctx, types.NamespacedName{Name: rns.Spec.BlockPoolName, Namespace: s.namespace}, blockPool)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s CephBlockPool. %v", blockPool.Name, err)
			}

			// SID for RamenDR
			storageID := calculateCephRbdStorageID(
				cephCluster.Status.CephStatus.FSID,
				rns.Name,
			)

			radosNamespace := cmp.Or(rns.Spec.Mirroring, &rookCephv1.RadosNamespaceMirroring{}).RemoteNamespace
			if radosNamespace != nil {
				peerCephfsid, err := getPeerCephFSID(
					ctx,
					s.client,
					mirroring.GetMirroringSecretName(blockPool.Name),
					blockPool.Namespace,
				)
				if err != nil {
					klog.Errorf("failed to get peer Ceph FSID. %v", err)
				}

				peerStorageID := calculateCephRbdStorageID(
					peerCephfsid,
					*radosNamespace,
				)

				storageIDs := []string{storageID, peerStorageID}
				slices.Sort(storageIDs)
				replicationID := util.CalculateMD5Hash(storageIDs)

				extR = append(extR,
					&pb.ExternalResource{
						Name: fmt.Sprintf("rbd-volumereplicationclass-%v", util.FnvHash(volumeReplicationClass5mSchedule)),
						Kind: "VolumeReplicationClass",
						Labels: map[string]string{
							ramenDRStorageIDLabelKey:     storageID,
							ramenDRReplicationIDLabelKey: replicationID,
							ramenMaintenanceModeLabelKey: "Failover",
						},
						Annotations: map[string]string{
							"replication.storage.openshift.io/is-default-class": "true",
						},
						Data: mustMarshal(&replicationv1alpha1.VolumeReplicationClassSpec{
							Parameters: map[string]string{
								"replication.storage.openshift.io/replication-secret-name": provisionerSecretName,
								"mirroringMode": "snapshot",
								// This is a temporary fix till we get the replication schedule to ocs-operator
								"schedulingInterval": volumeReplicationClass5mSchedule,
								"clusterID":          clientProfile,
							},
							Provisioner: util.RbdDriverName,
						}),
					},
					&pb.ExternalResource{
						Name: fmt.Sprintf("rbd-flatten-volumereplicationclass-%v", util.FnvHash(volumeReplicationClass5mSchedule)),
						Kind: "VolumeReplicationClass",
						Labels: map[string]string{
							ramenDRStorageIDLabelKey:     storageID,
							ramenDRReplicationIDLabelKey: replicationID,
							ramenMaintenanceModeLabelKey: "Failover",
							ramenDRFlattenModeLabelKey:   "force",
						},
						Data: mustMarshal(&replicationv1alpha1.VolumeReplicationClassSpec{
							Parameters: map[string]string{
								"replication.storage.openshift.io/replication-secret-name": provisionerSecretName,
								"mirroringMode": "snapshot",
								"flattenMode":   "force",
								// This is a temporary fix till we get the replication schedule to ocs-operator
								"schedulingInterval": volumeReplicationClass5mSchedule,
								"clusterID":          clientProfile,
							},
							Provisioner: util.RbdDriverName,
						}),
					},
				)
			}

			extR = append(extR,
				&pb.ExternalResource{
					Name: "ceph-rbd",
					Kind: "StorageClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(rbdStorageClassData),
				},
				&pb.ExternalResource{
					Name: "ceph-rbd",
					Kind: "VolumeSnapshotClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(map[string]string{
						"csi.storage.k8s.io/snapshotter-secret-name": provisionerSecretName,
					}),
				},
				&pb.ExternalResource{
					Name: fmt.Sprintf("%s-groupsnapclass", req.StorageClaimName),
					Kind: "VolumeGroupSnapshotClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(map[string]string{
						"csi.storage.k8s.io/group-snapshotter-secret-name": provisionerSecretName,
						"clusterID": clientProfile,
						"pool":      rns.Spec.BlockPoolName,
					}),
				},
				&pb.ExternalResource{
					Kind: "ClientProfile",
					Name: "ceph-rbd",
					Data: mustMarshal(&csiopv1a1.ClientProfileSpec{
						Rbd: &csiopv1a1.RbdConfigSpec{
							RadosNamespace: cephRes.Name,
						},
					})},
			)

		case "CephFilesystemSubVolumeGroup":
			subVolumeGroup := &rookCephv1.CephFilesystemSubVolumeGroup{}
			err := s.client.Get(ctx, types.NamespacedName{Name: cephRes.Name, Namespace: s.namespace}, subVolumeGroup)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s cephFilesystemSubVolumeGroup. %v", cephRes.Name, err)
			}

			provisionerSecretName := storageClaimCephCsiSecretName("provisioner", storageRequestHash)
			nodeSecretName := storageClaimCephCsiSecretName("node", storageRequestHash)
			cephfsStorageClassData := map[string]string{
				"clusterID":          getSubVolumeGroupClusterID(subVolumeGroup),
				"subvolumegroupname": subVolumeGroup.Name,
				"fsName":             subVolumeGroup.Spec.FilesystemName,
				"pool":               subVolumeGroup.GetLabels()[ocsv1alpha1.CephFileSystemDataPoolLabel],
				"csi.storage.k8s.io/provisioner-secret-name":       provisionerSecretName,
				"csi.storage.k8s.io/node-stage-secret-name":        nodeSecretName,
				"csi.storage.k8s.io/controller-expand-secret-name": provisionerSecretName,
			}

			storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
			if err != nil {
				return nil, err
			}
			var kernelMountOptions map[string]string
			for _, option := range strings.Split(util.GetCephFSKernelMountOptions(storageCluster), ",") {
				if kernelMountOptions == nil {
					kernelMountOptions = map[string]string{}
				}
				parts := strings.Split(option, "=")
				kernelMountOptions[parts[0]] = parts[1]
			}

			// SID for RamenDR
			storageID := calculateCephFsStorageID(
				cephCluster.Status.CephStatus.FSID,
				subVolumeGroup.Name,
			)

			extR = append(extR,
				&pb.ExternalResource{
					Name: "cephfs",
					Kind: "StorageClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(cephfsStorageClassData),
				},
				&pb.ExternalResource{
					Name: cephRes.Name,
					Kind: cephRes.Kind,
					Data: mustMarshal(map[string]string{
						"filesystemName": subVolumeGroup.Spec.FilesystemName,
					})},
				&pb.ExternalResource{
					Name: "cephfs",
					Kind: "VolumeSnapshotClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(map[string]string{
						"csi.storage.k8s.io/snapshotter-secret-name": provisionerSecretName,
					}),
				},
				&pb.ExternalResource{
					Name: fmt.Sprintf("%s-groupsnapclass", req.StorageClaimName),
					Kind: "VolumeGroupSnapshotClass",
					Labels: map[string]string{
						ramenDRStorageIDLabelKey: storageID,
					},
					Data: mustMarshal(map[string]string{
						"csi.storage.k8s.io/group-snapshotter-secret-name": provisionerSecretName,
						"fsName":    subVolumeGroup.Spec.FilesystemName,
						"clusterID": clientProfile,
					}),
				},
				&pb.ExternalResource{
					Kind: "ClientProfile",
					Name: "cephfs",
					Data: mustMarshal(&csiopv1a1.ClientProfileSpec{
						CephFs: &csiopv1a1.CephFsConfigSpec{
							SubVolumeGroup:     cephRes.Name,
							KernelMountOptions: kernelMountOptions,
						},
					})},
			)
		}
	}

	klog.Infof("successfully returned the storage class claim %q for %q", req.StorageClaimName, req.StorageConsumerUUID)
	return &pb.StorageClaimConfigResponse{ExternalResource: extR}, nil

}

// ReportStatus rpc call to check if a consumer can reach to the provider.
func (s *OCSProviderServer) ReportStatus(ctx context.Context, req *pb.ReportStatusRequest) (*pb.ReportStatusResponse, error) {
	// Update the status in storageConsumer CR
	klog.Infof("Client status report received: %+v", req)

	if req.ClientOperatorVersion == "" {
		req.ClientOperatorVersion = notAvailable
	} else {
		if _, err := semver.Parse(req.ClientOperatorVersion); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Malformed ClientOperatorVersion: %v", err)
		}
	}

	if req.ClientPlatformVersion == "" {
		req.ClientPlatformVersion = notAvailable
	} else {
		if _, err := semver.Parse(req.ClientPlatformVersion); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "Malformed ClientPlatformVersion: %v", err)
		}
	}

	if err := s.consumerManager.UpdateConsumerStatus(ctx, req.StorageConsumerUUID, req); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "Failed to update lastHeartbeat payload in the storageConsumer resource: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "Failed to update lastHeartbeat payload in the storageConsumer resource: %v", err)
	}

	storageConsumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to get storageConsumer resource: %v", err)
	}

	clientOperatorVersion, err := semver.Parse(req.ClientOperatorVersion)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "Malformed ClientOperatorVersion: %v", err)
	}

	channelName := ""
	_, notAdjusted := storageConsumer.GetAnnotations()[util.TicketAnnotation]
	if notAdjusted && (clientOperatorVersion.Major == 4 && clientOperatorVersion.Minor == 18) {
		// TODO (leelavg): need to be removed in 4.20
		// We have a new controller which maps the resources from 4.18 to 4.19 way of management,
		// until the resources are mapped we don't want connected client to be upgrading, we'll
		// relax the condition on knowing the resources are mapped in a separate PR
		channelName = "stable-4.18"
	} else {
		channel, err := s.getOCSSubscriptionChannel(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to construct status response: %v", err)
		}
		channelName = channel
	}

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		return nil, err
	}

	inMaintenanceMode, err := s.isSystemInMaintenanceMode(ctx)
	if err != nil {
		klog.Error(err)
		return nil, status.Errorf(codes.Internal, "Failed to get maintenance mode status.")
	}

	isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, storageConsumer)
	if err != nil {
		klog.Error(err)
		return nil, status.Errorf(codes.Internal, "Failed to get mirroring status for consumer.")
	}

	desiredClientConfigHash := getDesiredClientConfigHash(
		channelName,
		storageConsumer,
		isEncryptionInTransitEnabled(storageCluster.Spec.Network),
		inMaintenanceMode,
		isConsumerMirrorEnabled,
	)

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
		return "", fmt.Errorf("unable to find ocs-operator subscription")
	}
	return subscription.Spec.Channel, nil
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

	pubKey, err := s.getOnboardingValidationKey(ctx)
	if err != nil {
		klog.Errorf("failed to get public key to validate peer onboarding ticket %v", err)
		return nil, status.Errorf(codes.Internal, "failed to validate peer onboarding ticket")
	}

	onboardingToken, err := decodeAndValidateTicket(req.OnboardingToken, pubKey)
	if err != nil {
		klog.Errorf("Invalid onboarding token. %v", err)
		return nil, status.Errorf(codes.InvalidArgument, "invalid onboarding ticket")
	}

	if onboardingToken.SubjectRole != services.PeerRole {
		err := fmt.Errorf("invalid onboarding ticket for %q, expecting role %s found role %s", req.StorageClusterUID, services.PeerRole, onboardingToken.SubjectRole)
		klog.Error(err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	storageClusterPeer, err := s.storageClusterPeerManager.GetByPeerStorageClusterUID(ctx, types.UID(req.StorageClusterUID))
	if err != nil {
		klog.Error(err)
		return nil, status.Errorf(codes.NotFound, "Cannot find a storage cluster peer that meets all criteria")
	}

	klog.Infof("Found StorageClusterPeer %s for PeerStorageCluster", storageClusterPeer.Name)

	if storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePending && storageClusterPeer.Status.State != ocsv1.StorageClusterPeerStatePeered {
		return nil, status.Errorf(codes.NotFound, "Cannot find a storage cluster peer that meets all criteria")
	}

	return &pb.PeerStorageClusterResponse{}, nil
}

func (s *OCSProviderServer) RequestMaintenanceMode(ctx context.Context, req *pb.RequestMaintenanceModeRequest) (*pb.RequestMaintenanceModeResponse, error) {
	// Get storage consumer resource using UUID
	if req.Enable {
		err := s.consumerManager.AddAnnotation(ctx, req.StorageConsumerUUID, util.RequestMaintenanceModeAnnotation, "")
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to request Maintenance Mode for storageConsumer")
		}
	} else {
		err := s.consumerManager.RemoveAnnotation(ctx, req.StorageConsumerUUID, util.RequestMaintenanceModeAnnotation)
		if err != nil {
			klog.Error(err)
			return nil, fmt.Errorf("failed to disable Maintenance Mode for storageConsumer")
		}
	}

	return &pb.RequestMaintenanceModeResponse{}, nil
}

func (s *OCSProviderServer) GetStorageClientsInfo(ctx context.Context, req *pb.StorageClientsInfoRequest) (*pb.StorageClientsInfoResponse, error) {
	klog.Infof("GetStorageClientsInfo called with request: %s", req)

	response := &pb.StorageClientsInfoResponse{}
	for i := range req.ClientIDs {
		consumer, err := s.consumerManager.GetByClientID(ctx, req.ClientIDs[i])
		if err != nil {
			klog.Errorf("failed to get consumer with client id %v: %v", req.ClientIDs[i], err)
			response.Errors = append(response.Errors,
				&pb.StorageClientInfoError{
					ClientID: req.ClientIDs[i],
					Code:     pb.ErrorCode_Internal,
					Message:  "failed loading client information",
				},
			)
		}
		if consumer == nil {
			klog.Infof("no consumer found with client id %v", req.ClientIDs[i])
			continue
		}

		if !consumer.Spec.Enable {
			klog.Infof("consumer is not yet enaled skipping %v", req.ClientIDs[i])
			continue
		}

		idx := slices.IndexFunc(consumer.OwnerReferences, func(ref metav1.OwnerReference) bool {
			return ref.Kind == "StorageCluster"
		})
		if idx == -1 {
			klog.Infof("no owner found for consumer %v", req.ClientIDs[i])
			continue
		}
		owner := &consumer.OwnerReferences[idx]
		if owner.UID != types.UID(req.StorageClusterUID) {
			klog.Infof("storageCluster specified on the req does not own the client %v", req.ClientIDs[i])
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
			klog.Error(err)
			continue
		}

		clientInfo := &pb.ClientInfo{ClientID: req.ClientIDs[i]}

		consumerConfig := util.WrapStorageConsumerResourceMap(consumerConfigMap.Data)
		if consumerConfig.GetRbdRadosNamespaceName() != "" {
			clientInfo.RadosNamespace = consumerConfig.GetRbdRadosNamespaceName()
		}

		response.ClientsInfo = append(response.ClientsInfo, clientInfo)
	}

	return response, nil
}

func (s *OCSProviderServer) GetBlockPoolsInfo(ctx context.Context, req *pb.BlockPoolsInfoRequest) (*pb.BlockPoolsInfoResponse, error) {
	klog.Infof("GetBlockPoolsInfo called with request: %s", req)

	response := &pb.BlockPoolsInfoResponse{}
	for i := range req.BlockPoolNames {
		cephBlockPool := &rookCephv1.CephBlockPool{}
		cephBlockPool.Name = req.BlockPoolNames[i]
		cephBlockPool.Namespace = s.namespace
		err := s.client.Get(ctx, client.ObjectKeyFromObject(cephBlockPool), cephBlockPool)
		if kerrors.IsNotFound(err) {
			klog.Infof("blockpool %v not found", cephBlockPool.Name)
			continue
		} else if err != nil {
			klog.Errorf("failed to get blockpool %v: %v", cephBlockPool.Name, err)
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
				klog.Infof("bootstrap secret %v for blockpool %v not found", secret.Name, cephBlockPool.Name)
				continue
			} else if err != nil {
				errMsg := fmt.Sprintf(
					"failed to get bootstrap secret %s for CephBlockPool %s: %v",
					cephBlockPool.Status.Info[mirroringTokenKey],
					cephBlockPool.Name,
					err,
				)
				klog.Error(errMsg)
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

func getPeerCephFSID(ctx context.Context, cl client.Client, secretName, namespace string) (string, error) {
	secret := &corev1.Secret{}
	secret.Name = secretName
	secret.Namespace = namespace
	err := cl.Get(ctx, client.ObjectKeyFromObject(secret), secret)
	if err != nil {
		return "", err
	}
	decodeString, err := base64.StdEncoding.DecodeString(string(secret.Data["token"]))
	if err != nil {
		return "", err
	}
	token := struct {
		FSID string `json:"fsid"`
	}{}
	err = json.Unmarshal(decodeString, &token)
	if err != nil {
		return "", err
	}
	return token.FSID, nil
}

func (s *OCSProviderServer) getPeerStorageID(ctx context.Context, storageCluster *ocsv1.StorageCluster, namespace, radosnamespace string) (string, error) {
	blockPool := &rookCephv1.CephBlockPool{}
	blockPool.Name = util.GenerateNameForCephBlockPool(storageCluster.Name)
	blockPool.Namespace = namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(blockPool), blockPool); err != nil {
		return "", fmt.Errorf("failed to get %s CephBlockPool. %v", blockPool.Name, err)
	}

	peerCephfsid, err := getPeerCephFSID(
		ctx,
		s.client,
		mirroring.GetMirroringSecretName(blockPool.Name),
		blockPool.Namespace,
	)
	if err != nil {
		return "", fmt.Errorf("failed to get peer Ceph FSID. %v", err)
	}

	rns := &rookCephv1.CephBlockPoolRadosNamespace{}
	rns.Name = fmt.Sprintf("%s-%s", blockPool.Name, radosnamespace)
	if radosnamespace == util.ImplicitRbdRadosNamespaceName {
		rns.Name = fmt.Sprintf(
			"%s-builtin-%s",
			blockPool.Name,
			util.ImplicitRbdRadosNamespaceName[1:len(util.ImplicitRbdRadosNamespaceName)-1],
		)
	}
	rns.Namespace = namespace
	if err := s.client.Get(ctx, client.ObjectKeyFromObject(rns), rns); err != nil {
		return "", err
	}

	if rns.Spec.Mirroring == nil {
		return "", fmt.Errorf("failed to get mirroring details")
	}

	peerStorageID := calculateCephRbdStorageID(
		peerCephfsid,
		*rns.Spec.Mirroring.RemoteNamespace,
	)
	return peerStorageID, nil
}

func (s *OCSProviderServer) getKubeResources(ctx context.Context, consumer *ocsv1alpha1.StorageConsumer) ([]client.Object, error) {

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

	if consumer.Status.Client.Name == "" {
		return nil, fmt.Errorf("waiting for the first heart beat before sending the resources")
	}

	kubeResources := []client.Object{}
	kubeResources, err = s.appendCephConnectionKubeResources(ctx, kubeResources, consumer)
	if err != nil {
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

	kubeResources, err = s.appendVolumeSnapshotClassKubeResources(
		ctx,
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
		kubeResources,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
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
	)
	if err != nil {
		return nil, err
	}

	kubeResources, err = s.appendNoobaaKubeResources(
		ctx,
		kubeResources,
		consumer,
	)
	if err != nil {
		return nil, err
	}

	return kubeResources, nil
}

func (s *OCSProviderServer) appendCephConnectionKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]client.Object, error) {

	configmap := &v1.ConfigMap{}
	configmap.Name = monConfigMap
	configmap.Namespace = consumer.Namespace
	err := s.client.Get(ctx, client.ObjectKeyFromObject(configmap), configmap)
	if err != nil {
		return kubeResources, fmt.Errorf("failed to get %s configMap. %v", monConfigMap, err)
	}
	if configmap.Data["data"] == "" {
		return kubeResources, fmt.Errorf("configmap %s data is empty", monConfigMap)
	}

	monIps, err := extractMonitorIps(configmap.Data["data"])
	if err != nil {
		return kubeResources, fmt.Errorf("failed to extract monitor IPs from configmap %s: %v", monConfigMap, err)
	}

	kubeResources = append(
		kubeResources,
		&csiopv1a1.CephConnection{
			ObjectMeta: metav1.ObjectMeta{
				Name:      consumer.Status.Client.Name,
				Namespace: consumer.Status.Client.OperatorNamespace,
			},
			Spec: csiopv1a1.CephConnectionSpec{
				Monitors: monIps,
			},
		})
	return kubeResources, nil
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
	profileMap := make(map[string]*csiopv1a1.ClientProfile)

	rnsName := consumerConfig.GetRbdRadosNamespaceName()
	if rnsName == util.ImplicitRbdRadosNamespaceName {
		rnsName = ""
	}
	rbdClientProfileName := consumerConfig.GetRbdClientProfileName()
	if rbdClientProfileName != "" {
		rbdClientProfile := profileMap[rbdClientProfileName]
		if rbdClientProfile == nil {
			rbdClientProfile = &csiopv1a1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      rbdClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1a1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[rbdClientProfileName] = rbdClientProfile

		}
		rbdClientProfile.Spec.Rbd = &csiopv1a1.RbdConfigSpec{
			RadosNamespace: rnsName,
		}
	}

	cephFsClientProfileName := consumerConfig.GetCephFsClientProfileName()
	if cephFsClientProfileName != "" {
		cephFsClientProfile := profileMap[cephFsClientProfileName]
		if cephFsClientProfile == nil {
			cephFsClientProfile = &csiopv1a1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cephFsClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1a1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[cephFsClientProfileName] = cephFsClientProfile
		}
		cephFsClientProfile.Spec.CephFs = &csiopv1a1.CephFsConfigSpec{
			SubVolumeGroup:     consumerConfig.GetSubVolumeGroupName(),
			KernelMountOptions: kernelMountOptions,
			RadosNamespace:     ptr.To(consumerConfig.GetSubVolumeGroupRadosNamespaceName()),
		}
	}

	nfsClientProfileName := consumerConfig.GetCephFsClientProfileName()
	if nfsClientProfileName != "" {
		nfsClientProfile := profileMap[nfsClientProfileName]
		if nfsClientProfile == nil {
			nfsClientProfile = &csiopv1a1.ClientProfile{
				ObjectMeta: metav1.ObjectMeta{
					Name:      nfsClientProfileName,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1a1.ClientProfileSpec{
					CephConnectionRef: corev1.LocalObjectReference{Name: consumer.Status.Client.Name},
				},
			}
			profileMap[nfsClientProfileName] = nfsClientProfile
		}
		nfsClientProfile.Spec.Nfs = &csiopv1a1.NfsConfigSpec{}
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

	cephClients := []string{}
	if consumerConfig.GetCsiRbdProvisionerSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiRbdProvisionerSecretName())
	}
	if consumerConfig.GetCsiRbdNodeSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiRbdNodeSecretName())
	}
	if consumerConfig.GetCsiCephFsProvisionerSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiCephFsProvisionerSecretName())
	}
	if consumerConfig.GetCsiCephFsNodeSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiCephFsNodeSecretName())
	}
	if consumerConfig.GetCsiNfsProvisionerSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiNfsProvisionerSecretName())
	}
	if consumerConfig.GetCsiNfsNodeSecretName() != "" {
		cephClients = append(cephClients, consumerConfig.GetCsiNfsNodeSecretName())
	}

	for i := range cephClients {
		cephClient := &rookCephv1.CephClient{}
		cephClient.Name = cephClients[i]
		cephClient.Namespace = consumer.Namespace
		if err := s.client.Get(ctx, client.ObjectKeyFromObject(cephClient), cephClient); err != nil {
			return kubeResources, err
		}

		cephUserSecret := &v1.Secret{}
		cephUserSecret.Namespace = consumer.Namespace
		if cephClient.Status != nil &&
			cephClient.Status.Info["secretName"] != "" {
			cephUserSecret.Name = cephClient.Status.Info["secretName"]
		}
		if cephUserSecret.Name == "" {
			return kubeResources, fmt.Errorf("failed to find cephclient secret name")
		}

		if err := s.client.Get(ctx, client.ObjectKeyFromObject(cephUserSecret), cephUserSecret); err != nil {
			return kubeResources, fmt.Errorf("failed to get %s secret. %v", cephUserSecret, err)
		}

		cephUserSecret.Name = cephClients[i]
		cephUserSecret.Namespace = consumer.Status.Client.OperatorNamespace

		kubeResources = append(kubeResources, cephUserSecret)
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendStorageClassKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]client.Object, error) {
	scMap := map[string]func() *storagev1.StorageClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		scMap[util.GenerateNameForCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GenerateNameForCephBlockPool(storageCluster.Name),
				consumerConfig.GetCsiRbdProvisionerSecretName(),
				consumerConfig.GetCsiRbdNodeSecretName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				storageCluster.Spec.ManagedResources.CephBlockPools.DefaultStorageClass,
			)
		}
		scMap[util.GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultVirtRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GenerateNameForCephBlockPool(storageCluster.Name),
				consumerConfig.GetCsiRbdProvisionerSecretName(),
				consumerConfig.GetCsiRbdNodeSecretName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
			)
		}
		if kmsConfig, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, storageCluster, s.client); err == nil && kmsConfig != nil {
			kmsServiceName := kmsConfig.Data["KMS_SERVICE_NAME"]
			scMap[util.GenerateNameForEncryptedCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
				return util.NewDefaultEncryptedRbdStorageClass(
					consumerConfig.GetRbdClientProfileName(),
					util.GenerateNameForCephBlockPool(storageCluster.Name),
					consumerConfig.GetCsiRbdProvisionerSecretName(),
					consumerConfig.GetCsiRbdNodeSecretName(),
					consumer.Status.Client.OperatorNamespace,
					kmsServiceName,
					storageCluster.GetAnnotations()[defaults.KeyRotationEnableAnnotation] == "false",
				)
			}
		}
		scMap[util.GenerateNameForNonResilientCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultNonResilientRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.GetTopologyConstrainedPools(storageCluster),
				consumerConfig.GetCsiRbdProvisionerSecretName(),
				consumerConfig.GetCsiRbdNodeSecretName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				storageCluster.GetAnnotations()[defaults.KeyRotationEnableAnnotation] == "false",
			)
		}
	}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		scMap[util.GenerateNameForCephFilesystemStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultCephFsStorageClass(
				consumerConfig.GetCephFsClientProfileName(),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				consumerConfig.GetCsiCephFsProvisionerSecretName(),
				consumerConfig.GetCsiCephFsNodeSecretName(),
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
				consumerConfig.GetCsiNfsProvisionerSecretName(),
				consumerConfig.GetCsiNfsNodeSecretName(),
				consumer.Status.Client.OperatorNamespace,
			)
		}
	}
	for i := range consumer.Spec.StorageClasses {
		var storageClass *storagev1.StorageClass
		var err error
		storageClassName := consumer.Spec.StorageClasses[i].Name
		scGen := scMap[storageClassName]
		if scGen != nil {
			storageClass = scGen()
			storageClass.Name = storageClassName
		} else {
			storageClass, err = util.StorageClassFromExisting(
				ctx,
				s.client,
				storageClassName,
				consumer,
				consumerConfig,
				rbdStorageId,
				cephFsStorageId,
				cephFsStorageId,
			)
		}
		if kerrors.IsNotFound(err) {
			klog.Warningf("StorageClass with name %s doesn't exist in the cluster", storageClassName)
		} else if errors.Is(err, util.UnsupportedProvisioner) {
			klog.Warningf("Encountered unsupported provisioner in storage class %s", storageClassName)
		} else if storageClass == nil {
			klog.Warningf("The name %s does not points to a builtin or an existing storage class, skipping", storageClassName)
		} else {
			kubeResources = append(kubeResources, storageClass)
		}
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeSnapshotClassKubeResources(
	ctx context.Context,
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
				consumerConfig.GetCsiRbdProvisionerSecretName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
			)
		}
	}
	if consumerConfig.GetCephFsClientProfileName() != "" {
		vscMap[util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter)] = func() *snapapi.VolumeSnapshotClass {
			return util.NewDefaultCephFsSnapshotClass(
				consumerConfig.GetCephFsClientProfileName(),
				consumerConfig.GetCsiCephFsProvisionerSecretName(),
				consumer.Status.Client.OperatorNamespace,
				cephFsStorageId,
			)
		}
	}
	if consumerConfig.GetNfsClientProfileName() != "" {
		vscMap[util.GenerateNameForSnapshotClass(storageCluster.Name, util.NfsSnapshotter)] = func() *snapapi.VolumeSnapshotClass {
			return util.NewDefaultNfsSnapshotClass(
				consumerConfig.GetNfsClientProfileName(),
				consumerConfig.GetCsiNfsProvisionerSecretName(),
				consumer.Status.Client.OperatorNamespace,
			)
		}
	}
	for i := range consumer.Spec.VolumeSnapshotClasses {
		var snapshotClass *snapapi.VolumeSnapshotClass
		var err error
		snapshotClassName := consumer.Spec.VolumeSnapshotClasses[i].Name
		vscGen := vscMap[snapshotClassName]
		if vscGen != nil {
			snapshotClass = vscGen()
			snapshotClass.Name = snapshotClassName
		} else {
			snapshotClass, err = util.VolumeSnapshotClassFromExisting(
				ctx,
				s.client,
				snapshotClassName,
				consumer,
				consumerConfig,
				rbdStorageId,
				cephFsStorageId,
				cephFsStorageId,
			)
		}
		if kerrors.IsNotFound(err) {
			klog.Warningf("VolumeSnapshotClass with name %s doesn't exist in the cluster", snapshotClassName)
		} else if errors.Is(err, util.UnsupportedDriver) {
			klog.Warningf("Encountered unsupported driver in volume snapshot class %s", snapshotClassName)
		} else if snapshotClass == nil {
			klog.Warningf("The name %s does not points to a builtin or an existing volumesnapshot class, skipping", snapshotClassName)
		} else {
			kubeResources = append(kubeResources, snapshotClass)
		}
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeGroupSnapshotClassKubeResources(
	ctx context.Context,
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
				consumerConfig.GetCsiRbdProvisionerSecretName(),
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
				consumerConfig.GetCsiCephFsProvisionerSecretName(),
				consumer.Status.Client.OperatorNamespace,
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				cephFsStorageId,
			)
		}
	}
	for i := range consumer.Spec.VolumeGroupSnapshotClasses {
		var groupSnapshotClass *groupsnapapi.VolumeGroupSnapshotClass
		var err error
		groupSnapshotClassName := consumer.Spec.VolumeGroupSnapshotClasses[i].Name
		vgscGen := vgscMap[groupSnapshotClassName]
		if vgscGen != nil {
			groupSnapshotClass = vgscGen()
			groupSnapshotClass.Name = groupSnapshotClassName
		} else {
			groupSnapshotClass, err = util.VolumeGroupSnapshotClassFromExisting(
				ctx,
				s.client,
				groupSnapshotClassName,
				consumer,
				consumerConfig,
				rbdStorageId,
				cephFsStorageId,
				cephFsStorageId,
			)
		}
		if kerrors.IsNotFound(err) {
			klog.Warningf("VolumeGroupSnapshotClass with name %s doesn't exist in the cluster", groupSnapshotClassName)
		} else if errors.Is(err, util.UnsupportedDriver) {
			klog.Warningf("Encountered unsupported driver in volume group snapshot class %s", groupSnapshotClassName)
		} else if groupSnapshotClass == nil {
			klog.Warningf("The name %s does not points to a builtin or an existing volume group snapshot class, skipping", groupSnapshotClassName)
		} else {
			kubeResources = append(kubeResources, groupSnapshotClass)
		}
	}
	return kubeResources, nil
}

func (s *OCSProviderServer) appendVolumeReplicationClassKubeResources(
	ctx context.Context,
	kubeResources []client.Object,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId string,
) ([]client.Object, error) {
	if mirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, consumer); err != nil {
		return kubeResources, err
	} else if !mirrorEnabled {
		klog.Infof("skipping distribution of VolumeReplicationClass as mirroring is not enabled for the consumer")
		return kubeResources, nil
	}

	//TODO: this is a ugly hack to generate the peerStorageID for generating replicationID
	// peerStorageID should come from GetStorageClientInfo
	peerStorageID, err := s.getPeerStorageID(ctx, storageCluster, consumer.Namespace, consumerConfig.GetRbdRadosNamespaceName())
	if err != nil {
		return kubeResources, err
	}

	storageIDs := []string{rbdStorageId, peerStorageID}
	slices.Sort(storageIDs)
	replicationID := util.CalculateMD5Hash(storageIDs)

	for i := range consumer.Spec.VolumeReplicationClasses {
		replicationClassName := consumer.Spec.VolumeReplicationClasses[i].Name
		//TODO: The code is written under the assumption VRC name is exactly the same as the template name and there
		// is 1:1 mapping between template and vrc. The restriction will be relaxed in the future
		vrcTemplate := &templatev1.Template{}
		vrcTemplate.Name = replicationClassName
		vrcTemplate.Namespace = consumer.Namespace

		if err := s.client.Get(ctx, client.ObjectKeyFromObject(vrcTemplate), vrcTemplate); err != nil {
			return kubeResources, fmt.Errorf("failed to get VolumeReplicationClass template: %s, %v", replicationClassName, err)
		}

		if len(vrcTemplate.Objects) != 1 {
			return kubeResources, fmt.Errorf("unexpected number of Volume Replication Class found expected 1")
		}

		vrc := &replicationv1alpha1.VolumeReplicationClass{}
		if err := json.Unmarshal(vrcTemplate.Objects[0].Raw, vrc); err != nil {
			return kubeResources, fmt.Errorf("failed to unmarshall volume replication class: %s, %v", replicationClassName, err)

		}

		if vrc.Name != replicationClassName {
			return kubeResources, fmt.Errorf("volume replication class name mismatch: %s, %v", replicationClassName, vrc.Name)
		}

		switch vrc.Spec.Provisioner {
		case util.RbdDriverName:
			vrc.Spec.Parameters["replication.storage.openshift.io/replication-secret-name"] = consumerConfig.GetCsiRbdProvisionerSecretName()
			vrc.Spec.Parameters["replication.storage.openshift.io/replication-secret-namespace"] = consumer.Status.Client.OperatorNamespace
			vrc.Spec.Parameters["clusterID"] = consumerConfig.GetRbdClientProfileName()
			util.AddLabel(vrc, ramenDRStorageIDLabelKey, rbdStorageId)
			util.AddLabel(vrc, ramenMaintenanceModeLabelKey, "Failover")
			util.AddLabel(vrc, ramenDRReplicationIDLabelKey, replicationID)
		default:
			return kubeResources, fmt.Errorf("unsupported Provisioner for VolumeReplicationClass")
		}
		kubeResources = append(kubeResources, vrc)
	}

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
) ([]client.Object, error) {
	cbpList := &rookCephv1.CephBlockPoolList{}
	if err := s.client.List(ctx, cbpList, client.InNamespace(s.namespace)); err != nil {
		return kubeResources, fmt.Errorf("failed to list cephBlockPools in namespace. %v", err)
	}
	blockPoolMapping := []csiopv1a1.BlockPoolIdPair{}
	for i := range cbpList.Items {
		cephBlockPool := &cbpList.Items[i]
		remoteBlockPoolID := cephBlockPool.GetAnnotations()[util.BlockPoolMirroringTargetIDAnnotation]
		if remoteBlockPoolID != "" {
			localBlockPoolID := strconv.Itoa(cephBlockPool.Status.PoolID)
			blockPoolMapping = append(
				blockPoolMapping,
				csiopv1a1.BlockPoolIdPair{localBlockPoolID, remoteBlockPoolID},
			)
		}
	}

	if len(blockPoolMapping) > 0 {
		// This is an assumption and should go away when deprecating the StorageClaim API
		// The current proposal is to read the clientProfile name from the storageConsumer status and
		// the remote ClientProfile name should be fetched from the GetClientsInfo rpc
		clientName := consumer.Status.Client.Name
		clientProfileName := util.CalculateMD5Hash(fmt.Sprintf("%s-ceph-rbd", clientName))
		kubeResources = append(
			kubeResources,
			&csiopv1a1.ClientProfileMapping{
				ObjectMeta: metav1.ObjectMeta{
					Name:      consumer.Status.Client.Name,
					Namespace: consumer.Status.Client.OperatorNamespace,
				},
				Spec: csiopv1a1.ClientProfileMappingSpec{
					Mappings: []csiopv1a1.MappingsSpec{
						{
							LocalClientProfile:  clientProfileName,
							RemoteClientProfile: clientProfileName,
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
