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
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/platform"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
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
	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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

	remoteObcCreationAnnotationKey = "remote-obc-creation"
	remoteObcNameLabelKey          = "remote-obc-name"
	remoteObcNamespaceLabelKey     = "remote-obc-namespace"
	remoteObcUIDLabelKey           = "remote-obc-uid"
	storageConsumerNameLabelKey    = "storage-consumer-name"
	storageConsumerUUIDLabelKey    = "storage-consumer-uuid"
	prefixOfHashedName             = "remote-obc"

	s3EndpointsConfigMapLabelKey = "ocs.openshift.io/hub-s3-endpoints"
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
	alertStore                *alertStore
}

type stringPair [2]string

type sanitizeFlags struct {
	skipMetaData bool
	skipStatus   bool
}

type kubeObjectWithOpRecord struct {
	kubeObject  client.Object
	clientOp    pb.KubeClientOp
	subResource *pb.SubResource
}

func NewOCSProviderServer(ctx context.Context, namespace string) (*OCSProviderServer, error) {
	scheme, err := newScheme()
	if err != nil {
		return nil, fmt.Errorf("failed to create new scheme. %v", err)
	}

	config, err := config.GetConfig()
	if err != nil {
		return nil, err
	}

	cache, err := ctrlcache.New(config, ctrlcache.Options{
		Scheme: scheme,
		DefaultNamespaces: map[string]ctrlcache.Config{
			namespace: {},
		},
	})
	if err != nil {
		return nil, err
	}

	client, err := client.New(config, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: cache,
		},
	})
	if err != nil {
		return nil, err
	}

	logger := klog.FromContext(ctx).WithName("NewOCSProviderServer")

	consumerManager, err := newConsumerManager(ctx, client, cache, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new OCSConumer instance. %v", err)
	}

	logger.Info("starting cache")
	go func() {
		if err := cache.Start(ctx); err != nil {
			logger.Error(err, "failed to start cache")
			os.Exit(1)
		}
	}()

	if !cache.WaitForCacheSync(ctx) {
		panic("cache did not sync")
	}

	prometheusURL := "https://prometheus-k8s.openshift-monitoring.svc.cluster.local:9091"
	if isROSAHCP, err := platform.IsPlatformROSAHCP(); err != nil {
		// If platform detection hasn't run (e.g. in the provider-api binary),
		// fall through to the default OpenShift Prometheus URL.
		klog.V(1).InfoS("platform not detected, using default Prometheus URL", "error", err)
	} else if isROSAHCP {
		prometheusURL = fmt.Sprintf("https://prometheus.%s.svc.cluster.local:9339", namespace)
	}

	prometheusHTTPClient, err := newPrometheusHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("failed to create Prometheus HTTP client: %w", err)
	}
	ac := newAlertStore(prometheusURL, 1*time.Minute, prometheusHTTPClient)
	go ac.startPolling(ctx)

	return &OCSProviderServer{
		client:                    client,
		scheme:                    scheme,
		consumerManager:           consumerManager,
		storageClusterPeerManager: newStorageClusterPeerManager(client, namespace),
		namespace:                 namespace,
		alertStore:                ac,
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
		records, err := s.getKubeResources(ctx, logger, consumer)
		if err != nil {
			logger.Error(err, "failed to get kube resources")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		response := &pb.GetDesiredClientStateResponse{}

		for i := range records {
			kubeResource := records[i].kubeObject
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
			switch gvk.Kind {
			case "ObjectBucketClaim", "ObjectBucket":
				sanitizeKubeResource(
					kubeResource,
					sanitizeFlags{
						skipStatus: true,
					},
				)
			default:
				sanitizeKubeResource(
					kubeResource,
					sanitizeFlags{},
				)
			}
			kubeResourceBytes := util.JsonMustMarshal(kubeResource)
			response.KubeObjects = append(response.KubeObjects, &pb.KubeObject{
				Bytes:       kubeResourceBytes,
				Op:          records[i].clientOp,
				SubResource: records[i].subResource,
			})
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

		useHostNetworkForCtrlPlugin := util.ShouldUseHostNetworking(storageCluster)

		availableServices, err := util.GetAvailableServices(ctx, s.client, storageCluster)
		if err != nil {
			logger.Error(err, "failed to get available services")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		if availableServices.Rbd {
			response.RbdDriverRequirements = &pb.RbdDriverRequirements{}
			response.RbdDriverRequirements.CtrlPluginHostNetwork = ptr.To(useHostNetworkForCtrlPlugin)
		}
		if availableServices.CephFs {
			response.CephFsDriverRequirements = &pb.CephFsDriverRequirements{}
			response.CephFsDriverRequirements.CtrlPluginHostNetwork = ptr.To(useHostNetworkForCtrlPlugin)
		}
		if availableServices.Nfs {
			response.NfsDriverRequirements = &pb.NfsDriverRequirements{}
			response.NfsDriverRequirements.CtrlPluginHostNetwork = ptr.To(useHostNetworkForCtrlPlugin)
		}

		storageClassesResourceVersion, err := s.getStorageClassesResourceVersion(ctx)
		if err != nil {
			logger.Error(err, "failed to get storage class resource version")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		vSClassesResourceVersion, err := s.getVolumeSnapshotClassesResourceVersion(ctx)
		if err != nil {
			logger.Error(err, "failed to get volume snapshot class resource version")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}
		// TODO: enable vgsc after GA of API
		// vGSClassesResourceVersion, err := s.getVolumeGroupSnapshotClassesResourceVersion(ctx)
		// if err != nil {
		// 	logger.Error(err, "failed to get volume group snapshot class resource version")
		// 	return nil, status.Errorf(codes.Internal, "failed to produce client state")
		// }
		odfVGSClassesResourceVersion, err := s.getOdfVolumeGroupSnapshotClassesResourceVersion(ctx)
		if err != nil {
			logger.Error(err, "failed to get odf volume group class resource version")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		topologyKey := consumer.GetAnnotations()[util.AnnotationNonResilientPoolsTopologyKey]
		if topologyKey != "" {
			response.RbdDriverRequirements = &pb.RbdDriverRequirements{
				TopologyDomainLables: []string{topologyKey},
			}
		}

		obcResourceVersions, err := s.getOBCResourceVersions(ctx, logger, consumer)
		if err != nil {
			logger.Error(err, "failed to get hosted OBC resource versions for consumer", consumer.GetUID())
			return nil, status.Errorf(codes.Internal, "Failed to produce client state")
		}

		s3EndpointsListResourceVersion, err := s.getS3EndpointsListResourceVersion(ctx)
		if err != nil {
			logger.Error(err, "failed to get endpoints list resource version")
			return nil, status.Errorf(codes.Internal, "failed to produce client state")
		}

		desiredClientConfigHash := getDesiredClientConfigHash(
			channelName,
			zerodNonHashableFields(consumer),
			cephConnection.Spec,
			isEncryptionInTransitEnabled(storageCluster.Spec.Network),
			inMaintenanceMode,
			isConsumerMirrorEnabled,
			topologyKey,
			ocsVersion.Version,
			availableServices.Rbd,
			availableServices.CephFs,
			availableServices.Nfs,
			storageClassesResourceVersion,
			vSClassesResourceVersion,
			// TODO: enable vgs after GA of API
			// vGSClassesResourceVersion,
			odfVGSClassesResourceVersion,
			useHostNetworkForCtrlPlugin,
			obcResourceVersions,
			s3EndpointsListResourceVersion,
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

	storageClassesResourceVersion, err := s.getStorageClassesResourceVersion(ctx)
	if err != nil {
		logger.Error(err, "failed to get storage class resource version")
		return nil, status.Errorf(codes.Internal, "failed to produce client state")
	}
	vSClassesResourceVersion, err := s.getVolumeSnapshotClassesResourceVersion(ctx)
	if err != nil {
		logger.Error(err, "failed to get volume snapshot class resource version")
		return nil, status.Errorf(codes.Internal, "failed to produce client state")
	}
	// TODO: enable vgsc after GA of API
	// vGSClassesResourceVersion, err := s.getVolumeGroupSnapshotClassesResourceVersion(ctx)
	// if err != nil {
	// 	logger.Error(err, "failed to get volume group snapshot class resource version")
	// 	return nil, status.Errorf(codes.Internal, "failed to produce client state")
	// }
	odfVGSClassesResourceVersion, err := s.getOdfVolumeGroupSnapshotClassesResourceVersion(ctx)
	if err != nil {
		logger.Error(err, "failed to get odf volume group class resource version")
		return nil, status.Errorf(codes.Internal, "failed to produce client state")
	}

	obcResourceVersions, err := s.getOBCResourceVersions(ctx, logger, storageConsumer)
	if err != nil {
		logger.Error(err, "failed to get hosted OBC resource versions for consumer", storageConsumer.GetUID())
		return nil, status.Errorf(codes.Internal, "Failed to produce client state")
	}

	s3EndpointsListResourceVersion, err := s.getS3EndpointsListResourceVersion(ctx)
	if err != nil {
		logger.Error(err, "failed to get endpoints list resource version")
		return nil, status.Errorf(codes.Internal, "failed to produce client state")
	}

	desiredClientConfigHash := getDesiredClientConfigHash(
		channelName,
		zerodNonHashableFields(storageConsumer),
		cephConnection.Spec,
		isEncryptionInTransitEnabled(storageCluster.Spec.Network),
		inMaintenanceMode,
		isConsumerMirrorEnabled,
		topologyKey,
		ocsVersion.Version,
		availableServices.Rbd,
		availableServices.CephFs,
		availableServices.Nfs,
		storageClassesResourceVersion,
		vSClassesResourceVersion,
		// TODO: enable vgs
		// vGSClassesResourceVersion,
		odfVGSClassesResourceVersion,
		util.ShouldUseHostNetworking(storageCluster),
		obcResourceVersions,
		s3EndpointsListResourceVersion,
	)

	logger.Info("Successfully processed status report")
	return &pb.ReportStatusResponse{
		DesiredClientOperatorChannel: channelName,
		DesiredConfigHash:            desiredClientConfigHash,
	}, nil
}

func zerodNonHashableFields(consumer *ocsv1alpha1.StorageConsumer) *ocsv1alpha1.StorageConsumer {
	consumerCopy := &ocsv1alpha1.StorageConsumer{}
	consumerCopy.DeepCopyInto(consumer)
	consumerCopy.ManagedFields = nil
	consumerCopy.ResourceVersion = ""
	consumerCopy.Status.LastHeartbeat.Time = time.Time{}
	return consumerCopy
}

func getDesiredClientConfigHash(parts ...any) string {
	return util.CalculateMD5Hash(parts)
}

func compareStringPair(a, b stringPair) int {
	if r := strings.Compare(a[0], b[0]); r != 0 {
		return r
	} else {
		return strings.Compare(a[1], b[1])
	}
}

/*
getOBCResourceVersions returns a single list containing OBC, OB, ConfigMap and Secret ResourceVersions for a storage consumer
*/
func (s *OCSProviderServer) getOBCResourceVersions(ctx context.Context, logger logr.Logger, consumer *ocsv1alpha1.StorageConsumer) (resourceVersions []stringPair, err error) {
	obcList := &nbv1.ObjectBucketClaimList{}
	if err := s.client.List(
		ctx,
		obcList,
		client.InNamespace(consumer.Namespace),
		client.MatchingLabels{
			storageConsumerUUIDLabelKey: string(consumer.UID),
		},
	); err != nil {
		logger.Error(err, "failed to list OBC's for consumer")
		return nil, fmt.Errorf("failed to list OBC's for consumer. error is %v", err)
	}

	for i := range obcList.Items {
		obc := &obcList.Items[i]

		ob, configMap, secret, err := s.getOBCRelatedResources(ctx, obc.Name, consumer)
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("failed to get OBC related resources for consumer %v. %v", consumer.UID, err)
		}

		resourceVersions = append(
			resourceVersions,
			stringPair{"obc", obc.ResourceVersion},
		)

		if !kerrors.IsNotFound(err) {
			resourceVersions = append(
				resourceVersions,
				stringPair{"ob", ob.ResourceVersion},
				stringPair{"configmap", configMap.ResourceVersion},
				stringPair{"secret", secret.ResourceVersion},
			)
		}
	}

	slices.SortFunc(resourceVersions, compareStringPair)
	return
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
			clientInfo.RbdStorageID = util.CalculateCephRbdStorageID(fsid, rns)
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
	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		return false, err
	}
	val, exists := storageCluster.GetAnnotations()[util.InMaintenanceModeAnnotation]
	if !exists {
		return false, nil
	}
	return strconv.ParseBool(val)
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

func (s *OCSProviderServer) getKubeResources(ctx context.Context, logger logr.Logger, consumer *ocsv1alpha1.StorageConsumer) ([]kubeObjectWithOpRecord, error) {

	consumerConfigMap := &corev1.ConfigMap{}
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

	rbdStorageId := util.CalculateCephRbdStorageID(
		fsid,
		consumerConfig.GetRbdRadosNamespaceName(),
	)
	cephFsStorageId := util.CalculateCephFsStorageID(
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

	records := []kubeObjectWithOpRecord{}
	if cephConnection, err := s.getDesiredCephConnection(ctx, consumer, storageCluster); err == nil {
		records = append(records, kubeObjectWithOpRecord{
			kubeObject: cephConnection,
			clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
		})
	} else {
		return nil, err
	}

	records, err = s.appendClientProfileKubeResources(
		records,
		consumer,
		consumerConfig,
		storageCluster,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendCephClientSecretKubeResources(
		ctx,
		records,
		consumer,
		consumerConfig,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendStorageClassKubeResources(
		ctx,
		logger,
		records,
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

	records, err = s.appendVolumeSnapshotClassKubeResources(
		ctx,
		logger,
		records,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	// TODO: enable vgsc after GA of API
	// records, err = s.appendVolumeGroupSnapshotClassKubeResources(
	// 	ctx,
	// 	logger,
	// 	records,
	// 	consumer,
	// 	consumerConfig,
	// 	storageCluster,
	// 	rbdStorageId,
	// 	cephFsStorageId,
	// )
	// if err != nil {
	// 	return nil, err
	// }

	records, err = s.appendOdfVolumeGroupSnapshotClassKubeResources(
		ctx,
		logger,
		records,
		consumer,
		consumerConfig,
		storageCluster,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendNetworkFenceClassKubeResources(
		ctx,
		logger,
		records,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		cephFsStorageId,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendVolumeReplicationClassKubeResources(
		ctx,
		logger,
		records,
		consumer,
		consumerConfig,
		rbdStorageId,
		mirroringTargetInfo.RbdStorageID,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendVolumeGroupReplicationClassKubeResources(
		ctx,
		logger,
		records,
		consumer,
		consumerConfig,
		storageCluster,
		rbdStorageId,
		mirroringTargetInfo.RbdStorageID,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendClusterResourceQuotaKubeResources(
		records,
		consumer,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendClientProfileMappingKubeResources(
		ctx,
		records,
		consumer,
		consumerConfig,
		mirroringTargetInfo,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendOBCResources(
		ctx,
		records,
		consumer,
	)
	if err != nil {
		return nil, err
	}

	records, err = s.appendS3EndpointsListKubeResources(
		ctx,
		records,
		consumer,
	)
	if err != nil {
		return nil, err
	}

	return records, nil
}

func (s *OCSProviderServer) getDesiredCephConnection(
	ctx context.Context,
	consumer *ocsv1alpha1.StorageConsumer,
	storageCluster *ocsv1.StorageCluster,
) (*csiopv1.CephConnection, error) {

	configmap := &corev1.ConfigMap{}
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
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
) ([]kubeObjectWithOpRecord, error) {
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
		records = append(records, kubeObjectWithOpRecord{
			kubeObject: profileObj,
			clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
		})
	}
	return records, nil
}

func (s *OCSProviderServer) appendCephClientSecretKubeResources(
	ctx context.Context,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
) ([]kubeObjectWithOpRecord, error) {

	var err error
	if destSecretName := consumerConfig.GetCsiRbdProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiRbdProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiRbdNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiRbdNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiCephFsProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiCephFsProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiCephFsNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiCephFsNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiNfsProvisionerCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiNfsProvisionerCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}
	if destSecretName := consumerConfig.GetCsiNfsNodeCephUserName(); destSecretName != "" {
		srcSecretName := util.GenerateCsiNfsNodeCephClientName(csiCephUserCurrGen, consumer.UID)
		if records, err = s.appendCephClientSecretKubeResource(
			ctx,
			records,
			consumer,
			srcSecretName,
			destSecretName,
		); err != nil {
			return nil, err
		}
	}

	return records, nil
}

func (s *OCSProviderServer) appendCephClientSecretKubeResource(
	ctx context.Context,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	srcSecretName string,
	destSecretName string,
) ([]kubeObjectWithOpRecord, error) {
	cephUserSecret := &corev1.Secret{}
	cephUserSecret.Name = srcSecretName
	cephUserSecret.Namespace = consumer.Namespace

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(cephUserSecret), cephUserSecret); err != nil {
		return records, fmt.Errorf("failed to get %s secret. %v", cephUserSecret, err)
	}

	cephUserSecret.Name = destSecretName
	cephUserSecret.Namespace = consumer.Status.Client.OperatorNamespace
	// clearing the secretType to be empty/Opaque instead of type rook.
	cephUserSecret.Type = ""

	return append(records, kubeObjectWithOpRecord{
		kubeObject: cephUserSecret,
		clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
	}), nil
}

func (s *OCSProviderServer) appendStorageClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
	remoteRbdStorageId string,
) ([]kubeObjectWithOpRecord, error) {
	scMap := map[string]func() *storagev1.StorageClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		scMap[util.GenerateNameForCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.If(
					util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
					storageCluster.Spec.ManagedResources.CephBlockPools.ErasureCodedMetadataPool,
					util.GenerateNameForCephBlockPool(storageCluster.Name),
				),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumerConfig.GetCsiRbdNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				remoteRbdStorageId,
				storageCluster.Spec.ManagedResources.CephBlockPools.DefaultStorageClass,
				util.If(
					util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
					util.GenerateNameForCephBlockPool(storageCluster.Name),
					"",
				),
			)
		}
		scMap[util.GenerateNameForCephBlockPoolVirtualizationStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultVirtRbdStorageClass(
				consumerConfig.GetRbdClientProfileName(),
				util.If(
					util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
					storageCluster.Spec.ManagedResources.CephBlockPools.ErasureCodedMetadataPool,
					util.GenerateNameForCephBlockPool(storageCluster.Name),
				),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumerConfig.GetCsiRbdNodeCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
				remoteRbdStorageId,
				storageCluster.Spec.ManagedResources.CephBlockPools.DefaultVirtualizationStorageClass,
				util.If(
					util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
					util.GenerateNameForCephBlockPool(storageCluster.Name),
					"",
				),
			)
		}
		if kmsConfig, err := util.GetKMSConfigMap(defaults.KMSConfigMapName, storageCluster, s.client); err == nil && kmsConfig != nil {
			kmsServiceName := kmsConfig.Data["KMS_SERVICE_NAME"]
			scMap[util.GenerateNameForEncryptedCephBlockPoolStorageClass(storageCluster)] = func() *storagev1.StorageClass {
				return util.NewDefaultEncryptedRbdStorageClass(
					consumerConfig.GetRbdClientProfileName(),
					util.If(
						util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
						storageCluster.Spec.ManagedResources.CephBlockPools.ErasureCodedMetadataPool,
						util.GenerateNameForCephBlockPool(storageCluster.Name),
					),
					consumerConfig.GetCsiRbdProvisionerCephUserName(),
					consumerConfig.GetCsiRbdNodeCephUserName(),
					consumer.Status.Client.OperatorNamespace,
					kmsServiceName,
					consumer.GetAnnotations()[defaults.KeyRotationEnableAnnotation],
					util.If(
						util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
						util.GenerateNameForCephBlockPool(storageCluster.Name),
						"",
					),
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
				storageCluster.Spec.ManagedResources.CephFilesystems.DefaultStorageClassDataPoolName,
			)
		}
	}
	if consumerConfig.GetNfsClientProfileName() != "" {
		scMap[util.GenerateNameForCephNetworkFilesystemStorageClass(storageCluster)] = func() *storagev1.StorageClass {
			return util.NewDefaultNFSStorageClass(
				consumerConfig.GetNfsClientProfileName(),
				util.GenerateNameForCephNFS(storageCluster),
				util.GenerateNameForCephFilesystem(storageCluster.Name),
				util.GenerateNameForNFSServer(storageCluster),
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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendVolumeSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]kubeObjectWithOpRecord, error) {
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
	records = append(records, resources...)

	return records, nil
}

//nolint:unused
func (s *OCSProviderServer) appendVolumeGroupSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]kubeObjectWithOpRecord, error) {
	vgscMap := map[string]func() *groupsnapapi.VolumeGroupSnapshotClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		vgscMap[util.GenerateNameForGroupSnapshotClass(storageCluster, util.RbdGroupSnapshotter)] = func() *groupsnapapi.VolumeGroupSnapshotClass {
			return util.NewDefaultRbdGroupSnapshotClass(
				consumerConfig.GetRbdClientProfileName(),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				util.If(
					util.IsDefaultPoolErasureCodingEnabled(storageCluster.Spec.ManagedResources.CephBlockPools),
					storageCluster.Spec.ManagedResources.CephBlockPools.ErasureCodedMetadataPool,
					util.GenerateNameForCephBlockPool(storageCluster.Name),
				),
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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendOdfVolumeGroupSnapshotClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	cephFsStorageId string,
) ([]kubeObjectWithOpRecord, error) {
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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendNetworkFenceClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId,
	cephFsStorageId string,
) ([]kubeObjectWithOpRecord, error) {
	nfcMap := map[string]func() *csiaddonsv1alpha1.NetworkFenceClass{}
	if consumerConfig.GetRbdClientProfileName() != "" {
		nfcMap[util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.RbdNetworkFenceClass)] = func() *csiaddonsv1alpha1.NetworkFenceClass {
			return util.NewDefaultRbdNetworkFenceClass(
				consumerConfig.GetRbdClientProfileName(),
				consumerConfig.GetCsiRbdProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				rbdStorageId,
			)
		}
	}

	if consumerConfig.GetCephFsClientProfileName() != "" {
		nfcMap[util.GenerateNameForNetworkFenceClass(storageCluster.Name, util.CephfsNetworkFenceClass)] = func() *csiaddonsv1alpha1.NetworkFenceClass {
			return util.NewDefaultCephfsNetworkFenceClass(
				consumerConfig.GetCephFsClientProfileName(),
				consumerConfig.GetCsiCephFsProvisionerCephUserName(),
				consumer.Status.Client.OperatorNamespace,
				cephFsStorageId,
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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendVolumeReplicationClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	rbdStorageId string,
	remoteRbdStorageId string,
) ([]kubeObjectWithOpRecord, error) {

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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendVolumeGroupReplicationClassKubeResources(
	ctx context.Context,
	logger logr.Logger,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	storageCluster *ocsv1.StorageCluster,
	rbdStorageId string,
	remoteRbdStorageId string,
) ([]kubeObjectWithOpRecord, error) {

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
	records = append(records, resources...)

	return records, nil
}

func (s *OCSProviderServer) appendClusterResourceQuotaKubeResources(
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]kubeObjectWithOpRecord, error) {
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

		records = append(records, kubeObjectWithOpRecord{
			kubeObject: clusterResourceQuota,
			clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
		})
	}
	return records, nil
}

func (s *OCSProviderServer) appendClientProfileMappingKubeResources(
	ctx context.Context,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
	consumerConfig util.StorageConsumerResources,
	mirroringTargetInfo *pb.ClientInfo,
) ([]kubeObjectWithOpRecord, error) {
	cbpList := &rookCephv1.CephBlockPoolList{}
	if err := s.client.List(ctx, cbpList, client.InNamespace(s.namespace)); err != nil {
		return records, fmt.Errorf("failed to list cephBlockPools in namespace. %v", err)
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
		clientProfileMapping := &csiopv1.ClientProfileMapping{
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
		}

		records = append(
			records,
			kubeObjectWithOpRecord{
				kubeObject: clientProfileMapping,
				clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
			},
		)
	}
	return records, nil
}

func (s *OCSProviderServer) appendOBCResources(
	ctx context.Context,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]kubeObjectWithOpRecord, error) {

	obcList := &nbv1.ObjectBucketClaimList{}
	if err := s.client.List(
		ctx,
		obcList,
		client.InNamespace(consumer.Namespace),
		client.MatchingLabels{
			storageConsumerUUIDLabelKey: string(consumer.UID),
		},
	); err != nil {
		return nil, fmt.Errorf("failed to list OBCs for consumer %v. %v", consumer.UID, err)
	}

	// OB, ConfigMap and Secrets can be obtained by using the OBC names
	for i := range obcList.Items {
		obc := &obcList.Items[i]
		remoteOBCName := obc.Labels[remoteObcNameLabelKey]
		remoteOBCNamespace := obc.Labels[remoteObcNamespaceLabelKey]

		ob, configMap, secret, err := s.getOBCRelatedResources(ctx, obc.Name, consumer)
		if client.IgnoreNotFound(err) != nil {
			return nil, fmt.Errorf("failed to get OBC related resources for consumer %v. %v", consumer.UID, err)
		}

		obc.Name = remoteOBCName
		obc.Namespace = remoteOBCNamespace
		obc.Annotations = nil
		obc.Labels = nil
		statusSubResource := pb.SubResource_SUB_RESOURCE_STATUS

		records = append(records,
			kubeObjectWithOpRecord{
				kubeObject:  obc,
				clientOp:    pb.KubeClientOp_UPDATE_SUB_RESOURCE,
				subResource: &statusSubResource,
			},
		)

		if kerrors.IsNotFound(err) {
			return records, nil
		}

		ob.Name = fmt.Sprintf("obc-%s-%s", remoteOBCNamespace, remoteOBCName)
		configMap.Name = remoteOBCName
		configMap.Namespace = remoteOBCNamespace
		secret.Name = remoteOBCName
		secret.Namespace = remoteOBCNamespace

		records = append(records,
			kubeObjectWithOpRecord{
				kubeObject: ob,
				clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
			},
			kubeObjectWithOpRecord{
				kubeObject:  ob,
				clientOp:    pb.KubeClientOp_UPDATE_SUB_RESOURCE,
				subResource: &statusSubResource,
			},
			kubeObjectWithOpRecord{
				kubeObject: configMap,
				clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
			},
			kubeObjectWithOpRecord{
				kubeObject: secret,
				clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
			},
		)
	}

	return records, nil
}

func (s *OCSProviderServer) getOBCRelatedResources(
	ctx context.Context,
	obcName string,
	consumer *ocsv1alpha1.StorageConsumer,
) (*nbv1.ObjectBucket, *corev1.ConfigMap, *corev1.Secret, error) {

	ob := &nbv1.ObjectBucket{}
	ob.Name = fmt.Sprintf("obc-%s-%s", consumer.Namespace, obcName)
	if err := s.client.Get(
		ctx,
		client.ObjectKeyFromObject(ob),
		ob,
	); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get OB for OBC %s: %w", obcName, err)
	}

	configMap := &corev1.ConfigMap{}
	configMap.Namespace = consumer.Namespace
	configMap.Name = obcName
	if err := s.client.Get(
		ctx,
		client.ObjectKeyFromObject(configMap),
		configMap,
	); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get ConfigMap for OBC %s: %w", obcName, err)
	}

	secret := &corev1.Secret{}
	secret.Namespace = consumer.Namespace
	secret.Name = obcName
	if err := s.client.Get(
		ctx,
		client.ObjectKeyFromObject(secret),
		secret,
	); err != nil {
		return nil, nil, nil, fmt.Errorf("failed to get Secret for OBC %s: %w", obcName, err)
	}

	return ob, configMap, secret, nil
}

func (s *OCSProviderServer) getResourceVersions(
	ctx context.Context,
	list client.ObjectList,
	filterAndCollect func() []string,
) ([]string, error) {
	if err := s.client.List(ctx, list, &client.MatchingLabelsSelector{
		Selector: util.GetExternalClassesBlacklistSelector(),
	}); meta.IsNoMatchError(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	resourceVersions := filterAndCollect()
	slices.Sort(resourceVersions)
	return resourceVersions, nil
}

func (s *OCSProviderServer) getStorageClassesResourceVersion(ctx context.Context) ([]string, error) {
	list := &storagev1.StorageClassList{}
	return s.getResourceVersions(ctx, list, func() []string {
		var versions []string
		for i := range list.Items {
			if slices.Contains(util.SupportedCsiDrivers, list.Items[i].Provisioner) {
				versions = append(versions, list.Items[i].ResourceVersion)
			}
		}
		return versions
	})
}

func (s *OCSProviderServer) getVolumeSnapshotClassesResourceVersion(ctx context.Context) ([]string, error) {
	list := &snapapi.VolumeSnapshotClassList{}
	return s.getResourceVersions(ctx, list, func() []string {
		var versions []string
		for i := range list.Items {
			if slices.Contains(util.SupportedCsiDrivers, list.Items[i].Driver) {
				versions = append(versions, list.Items[i].ResourceVersion)
			}
		}
		return versions
	})
}

//nolint:unused
func (s *OCSProviderServer) getVolumeGroupSnapshotClassesResourceVersion(ctx context.Context) ([]string, error) {
	list := &groupsnapapi.VolumeGroupSnapshotClassList{}
	return s.getResourceVersions(ctx, list, func() []string {
		var versions []string
		for i := range list.Items {
			if slices.Contains(util.SupportedCsiDrivers, list.Items[i].Driver) {
				versions = append(versions, list.Items[i].ResourceVersion)
			}
		}
		return versions
	})
}

func (s *OCSProviderServer) getOdfVolumeGroupSnapshotClassesResourceVersion(ctx context.Context) ([]string, error) {
	list := &odfgsapiv1b1.VolumeGroupSnapshotClassList{}
	return s.getResourceVersions(ctx, list, func() []string {
		var versions []string
		for i := range list.Items {
			if slices.Contains(util.SupportedCsiDrivers, list.Items[i].Driver) {
				versions = append(versions, list.Items[i].ResourceVersion)
			}
		}
		return versions
	})
}

func (s *OCSProviderServer) getS3EndpointsListResourceVersion(ctx context.Context) (string, error) {
	ocsConfigMap := &corev1.ConfigMap{}
	ocsConfigMap.Name = util.OcsHubS3EndpointsConfigMapName
	ocsConfigMap.Namespace = s.namespace
	configMapKey := client.ObjectKeyFromObject(ocsConfigMap)
	if err := s.client.Get(ctx, configMapKey, ocsConfigMap); err != nil {
		if kerrors.IsNotFound(err) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get ConfigMap %s: %w", configMapKey, err)
	}
	return ocsConfigMap.ResourceVersion, nil
}

func (s *OCSProviderServer) appendS3EndpointsListKubeResources(
	ctx context.Context,
	records []kubeObjectWithOpRecord,
	consumer *ocsv1alpha1.StorageConsumer,
) ([]kubeObjectWithOpRecord, error) {
	ocsConfigMap := &corev1.ConfigMap{}
	ocsConfigMap.Name = util.OcsHubS3EndpointsConfigMapName
	ocsConfigMap.Namespace = s.namespace
	configMapKey := client.ObjectKeyFromObject(ocsConfigMap)
	if err := s.client.Get(ctx, configMapKey, ocsConfigMap); err != nil {
		if kerrors.IsNotFound(err) {
			return records, nil
		}
		return nil, fmt.Errorf("failed to get ConfigMap %s: %w", configMapKey, err)
	}

	clientConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf(util.ClientS3EndpointsConfigMapNameFormat, consumer.Status.Client.ID),
			Namespace: consumer.Status.Client.OperatorNamespace,
			Labels: map[string]string{
				s3EndpointsConfigMapLabelKey: strconv.FormatBool(true),
			},
		},
		Data: ocsConfigMap.Data,
	}

	return append(records, kubeObjectWithOpRecord{
		kubeObject: clientConfigMap,
		clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
	}), nil
}

func sanitizeKubeResource(obj client.Object, sanitizeFlags sanitizeFlags) {
	name := obj.GetName()
	namespace := obj.GetNamespace()
	labels := obj.GetLabels()
	annotations := obj.GetAnnotations()

	if !sanitizeFlags.skipMetaData {
		zeroFieldByName(obj, "ObjectMeta")
	}

	if !sanitizeFlags.skipStatus {
		zeroFieldByName(obj, "Status")
	}

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
) []kubeObjectWithOpRecord {
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

	records := []kubeObjectWithOpRecord{}
	srcClassCache := map[string]client.Object{}
	for destName, srcName := range classNameMapping {
		var srcKubeObj client.Object
		if srcKubeObj = srcClassCache[srcName]; srcKubeObj == nil {
			var err error
			srcKubeObj, err = genClassKubeObjFn(srcName)
			if kerrors.IsNotFound(err) {
				logger.Info("Resource with name doesn't exist in the cluster", "Resource", classDisplayName, "Name", srcName)
			} else if errors.Is(err, util.ErrUnsupportedProvisioner) {
				logger.Info("Encountered unsupported provisioner", "Resource", classDisplayName, "Name", srcName)
			} else if errors.Is(err, util.ErrUnsupportedDriver) {
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
			records = append(records, kubeObjectWithOpRecord{
				kubeObject: distKubeObj,
				clientOp:   pb.KubeClientOp_CREATE_OR_UPDATE,
			})
		}
	}
	return records
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

// Notify RPC call for Notify API request
func (s *OCSProviderServer) Notify(ctx context.Context, req *pb.NotifyRequest) (*pb.NotifyResponse, error) {
	logger := klog.FromContext(ctx).WithName("Notify").WithValues("StorageConsumerUUID", req.StorageConsumerUUID)
	logger.Info("Starting Notify RPC", "reason", req.Reason)

	storageConsumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		logger.Error(err, "Failed to get StorageConsumer")
		return nil, status.Errorf(codes.Internal, "failed to get StorageConsumer: storageConsumerUUID=%s", req.StorageConsumerUUID)
	}

	switch req.Reason {
	case pb.NotifyReason_OBC_CREATED:
		obc := &nbv1.ObjectBucketClaim{}
		if err := json.Unmarshal(req.Payload, obc); err != nil {
			logger.Error(err, "Failed to unmarshal OBC created payload")
			return nil, status.Errorf(codes.InvalidArgument, "failed to unmarshal OBC create payload: %v", err)
		}
		if err := s.handleObcCreated(ctx, storageConsumer, obc); err != nil {
			logger.Error(err, "Failed to handle OBC creation")
			return nil, err
		}
	case pb.NotifyReason_OBC_DELETED:
		var obcNamespacedName types.NamespacedName
		if err := json.Unmarshal(req.Payload, &obcNamespacedName); err != nil {
			logger.Error(err, "Failed to unmarshal OBC deleted payload")
			return nil, status.Errorf(codes.InvalidArgument, "failed to unmarshal OBC delete payload: %v", err)
		}
		if err := s.handleObcDeleted(ctx, storageConsumer, obcNamespacedName); err != nil {
			logger.Error(err, "Failed to handle OBC deletion")
			return nil, err
		}
	default:
		return nil, status.Errorf(codes.InvalidArgument, "failed to find known reason in the Notify RPC request")
	}
	logger.Info("Successfully completed Notify RPC", "reason", req.Reason)
	return &pb.NotifyResponse{}, nil
}

// handleObcCreated create the OBC that the client cluster asked for on the provider cluster.
// we use createOrUpdate to handle the case where the OBC already exists and needs to be updated
// It is a synchronous call, we do not wait for resources to be created.
// Notes:
//   - OBC is created in the storage consumer namespace (and not the provider server namespace in case it would be moved)
//   - The OBC is named with an obscure name to avoid collisions
//   - Owner reference is set to the storage consumer
//   - Label added: indicates that the information is about the client cluster OBC
//   - Annotations added: "remote-obc-creation": "true" (used by MCG CLI)
func (s *OCSProviderServer) handleObcCreated(
	ctx context.Context,
	storageConsumer *ocsv1alpha1.StorageConsumer,
	obc *nbv1.ObjectBucketClaim,
) error {
	storageConsumerUUID := string(storageConsumer.UID)
	logger := klog.
		FromContext(ctx).
		WithName("handleObcCreated").
		WithValues(
			"storageConsumerUUID",
			storageConsumerUUID,
			"storageConsumer name",
			storageConsumer.Name,
		)

	obcName := obc.Name
	obcNamespace := obc.Namespace
	logger.Info("Starting handleObcCreated", "remote OBC Name", obcName, "remote OBC Namespace", obcNamespace)

	localObc := &nbv1.ObjectBucketClaim{}
	localObc.Name = getObcHashedName(client.ObjectKeyFromObject(storageConsumer), obcName, obcNamespace)
	localObc.Namespace = storageConsumer.Namespace

	logger.Info("CreateOrUpdate OBC object", "OBC Name", localObc.Name, "OBC Namespace", localObc.Namespace)
	if _, err := ctrl.CreateOrUpdate(ctx, s.client, localObc, func() error {
		if err := controllerutil.SetOwnerReference(storageConsumer, localObc, s.scheme); err != nil {
			return status.Errorf(codes.Internal, "failed to set owner reference for OBC name %s namespace %s: %v", obcName, obcNamespace, err)
		}

		if localObc.Labels == nil {
			localObc.Labels = map[string]string{}
		}
		localObc.Labels[storageConsumerNameLabelKey] = storageConsumer.Name
		localObc.Labels[storageConsumerUUIDLabelKey] = storageConsumerUUID
		localObc.Labels[remoteObcNameLabelKey] = obcName
		localObc.Labels[remoteObcNamespaceLabelKey] = obcNamespace
		localObc.Labels[remoteObcUIDLabelKey] = string(obc.UID)

		if localObc.Annotations == nil {
			localObc.Annotations = map[string]string{}
		}
		localObc.Annotations[remoteObcCreationAnnotationKey] = "true"

		localObc.Spec = obc.Spec // shadow copy, under the assumption that that the OBC was send for creation only and would not be used in other places
		return nil
	}); err != nil {
		return status.Errorf(codes.Internal, "failed to create/update OBC name %s namespace %s: %v", obcName, obcNamespace, err)
	}
	return nil
}

// handleObcDeleted delete the OBC that the client cluster asked for on the provider cluster.
// It is a synchronous call, we do not wait for resources to be deleted.
// Notes:
//   - OBC is deleted from the storage consumer namespace using the labels set during creation.
func (s *OCSProviderServer) handleObcDeleted(
	ctx context.Context,
	storageConsumer *ocsv1alpha1.StorageConsumer,
	obcNamespacedName types.NamespacedName,
) error {
	storageConsumerUUID := string(storageConsumer.UID)
	logger := klog.
		FromContext(ctx).
		WithName("handleObcDeleted").
		WithValues(
			"storageConsumerUUID",
			storageConsumerUUID,
			"storageConsumer name",
			storageConsumer.Name,
		)

	obcName := obcNamespacedName.Name
	obcNamespace := obcNamespacedName.Namespace
	logger.Info("Starting handleObcDeleted", "remote OBC Name", obcName, "remote OBC Namespace", obcNamespace)

	labelSelector := map[string]string{
		remoteObcNameLabelKey:       obcName,
		remoteObcNamespaceLabelKey:  obcNamespace,
		storageConsumerNameLabelKey: storageConsumer.Name,
	}
	localObcNamespace := storageConsumer.Namespace
	obcList := &nbv1.ObjectBucketClaimList{}
	if err := s.client.List(ctx, obcList, client.InNamespace(localObcNamespace), client.MatchingLabels(labelSelector), client.Limit(1)); err != nil {
		logger.Error(err, "Failed to list OBC resources", "namespace", localObcNamespace, "labels", labelSelector)
		return status.Errorf(codes.Internal, "failed to list OBCs for deletion name %s namespace %s: %v", obcName, obcNamespace, err)
	}
	if len(obcList.Items) == 0 {
		logger.Info("OBC not found", "namespace", localObcNamespace, "labels", labelSelector)
		return nil
	}

	localObc := &obcList.Items[0]
	logger.Info("Deleting OBC resource", "namespaced/name", client.ObjectKeyFromObject(localObc))
	if err := s.client.Delete(ctx, localObc); client.IgnoreNotFound(err) != nil {
		return status.Errorf(codes.Internal, "failed to delete OBC name %s namespace %s: %v", obcName, obcNamespace, err)
	}
	return nil
}

// getObcHashedName creates a stable hash for OBC name
// obcName and obcNamespace are from the client cluster
func getObcHashedName(
	storageConsumerNamespacedName types.NamespacedName,
	obcName string,
	obcNamespace string,
) string {
	s := []any{
		storageConsumerNamespacedName.Namespace,
		storageConsumerNamespacedName.Name,
		obcName,
		obcNamespace,
	}
	obcHash := util.JsonMustMarshal(s)
	md5Sum := md5.Sum(obcHash)
	hashString := hex.EncodeToString(md5Sum[:16])
	return fmt.Sprintf("%s-%s", prefixOfHashedName, hashString)
}

// GetClientAlerts returns cached alerts for a specific storage consumer.
// Alerts are updated in the background every minute, so this RPC returns immediately.
func (s *OCSProviderServer) GetClientAlerts(ctx context.Context, req *pb.GetClientAlertsRequest) (*pb.GetClientAlertsResponse, error) {
	logger := klog.FromContext(ctx).WithName("GetClientAlerts").WithValues("StorageConsumerUUID", req.StorageConsumerUUID)
	logger.Info("Starting GetClientAlerts RPC")

	if req.StorageConsumerUUID == "" {
		logger.Error(fmt.Errorf("missing required field"), "StorageConsumerUUID is empty")
		return nil, status.Errorf(codes.InvalidArgument, "StorageConsumerUUID is required")
	}

	storageConsumer, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		logger.Error(err, "Failed to get StorageConsumer")
		return nil, status.Errorf(codes.Internal, "failed to get StorageConsumer: storageConsumerUUID=%s", req.StorageConsumerUUID)
	}

	alerts, err := s.alertStore.getAlertsForConsumer(storageConsumer.Name)
	if err != nil {
		logger.Error(err, "Alert cache unavailable")
		return nil, status.Errorf(codes.Internal, "failed to get alerts: %v", err)
	}

	logger.Info("Successfully completed GetClientAlerts RPC", "alertCount", len(alerts))
	return &pb.GetClientAlertsResponse{Alerts: alerts}, nil
}
