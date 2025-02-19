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
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/api/v4"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/mirroring"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"
	ocsVersion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	csiopv1a1 "github.com/ceph/ceph-csi-operator/api/v1alpha1"
	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	nbapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/api/core/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	klog "k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	TicketAnnotation          = "ocs.openshift.io/provider-onboarding-ticket"
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
	storageCluster, err := util.GetStorageClusterInNamespace(ctx, s.client, s.namespace)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get storageCluster. %v", err)
	}

	if storageCluster.UID != onboardingTicket.StorageCluster {
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket storageCluster not match existing storageCluster.")
	}

	if !storageCluster.Spec.AllowRemoteStorageConsumers {
		return nil, status.Errorf(codes.PermissionDenied, "onboarding remote storageConsumer(s) is not allowed.")
	}

	storageQuotaInGiB := ptr.Deref(onboardingTicket.StorageQuotaInGiB, 0)

	if onboardingTicket.SubjectRole != services.ClientRole {
		err := fmt.Errorf("invalid onboarding ticket for %q, expecting role %s found role %s", req.ConsumerName, services.ClientRole, onboardingTicket.SubjectRole)
		klog.Error(err)
		return nil, status.Error(codes.InvalidArgument, err.Error())
	}

	storageConsumerUUID, err := s.consumerManager.Create(ctx, req, int(storageQuotaInGiB))
	if err != nil {
		if !kerrors.IsAlreadyExists(err) && err != errTicketAlreadyExists {
			return nil, status.Errorf(codes.Internal, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
		}

		storageConsumer, err := s.consumerManager.GetByName(ctx, req.ConsumerName)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get storageConsumer. %v", err)
		}

		if storageConsumer.Spec.Enable {
			err = fmt.Errorf("storageconsumers.ocs.openshift.io %s already exists", req.ConsumerName)
			return nil, status.Errorf(codes.AlreadyExists, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
		}
		storageConsumerUUID = string(storageConsumer.UID)
	}

	return &pb.OnboardConsumerResponse{StorageConsumerUUID: storageConsumerUUID}, nil
}

// AcknowledgeOnboarding acknowledge the onboarding is complete
func (s *OCSProviderServer) AcknowledgeOnboarding(ctx context.Context, req *pb.AcknowledgeOnboardingRequest) (*pb.AcknowledgeOnboardingResponse, error) {

	if err := s.consumerManager.EnableStorageConsumer(ctx, req.StorageConsumerUUID); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "storageConsumer not found. %v", err)
		}
		return nil, status.Errorf(codes.Internal, "Failed to update the storageConsumer. %v", err)
	}
	return &pb.AcknowledgeOnboardingResponse{}, nil
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
	case ocsv1alpha1.StorageConsumerStateDisabled:
		return nil, status.Errorf(codes.FailedPrecondition, "storageConsumer is in disabled state")
	case ocsv1alpha1.StorageConsumerStateFailed:
		// TODO: get correct error message from the storageConsumer status
		return nil, status.Errorf(codes.Internal, "storageConsumer status failed")
	case ocsv1alpha1.StorageConsumerStateConfiguring:
		return nil, status.Errorf(codes.Unavailable, "waiting for the rook resources to be provisioned")
	case ocsv1alpha1.StorageConsumerStateDeleting:
		return nil, status.Errorf(codes.NotFound, "storageConsumer is already in deleting phase")
	case ocsv1alpha1.StorageConsumerStateReady:
		conString, err := s.getExternalResources(ctx, consumerObj)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "failed to get external resources. %v", err)
		}

		channelName, err := s.getOCSSubscriptionChannel(ctx)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to construct status response: %v", err)
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

		isConsumerMirrorEnabled, err := s.isConsumerMirrorEnabled(ctx, consumerObj)
		if err != nil {
			klog.Error(err)
			return nil, status.Errorf(codes.Internal, "Failed to get mirroring status for consumer.")
		}

		desiredClientConfigHash := getDesiredClientConfigHash(
			channelName,
			consumerObj,
			isEncryptionInTransitEnabled(storageCluster.Spec.Network),
			inMaintenanceMode,
			isConsumerMirrorEnabled,
		)

		klog.Infof("successfully returned the config details to the consumer.")
		return &pb.StorageConfigResponse{
				ExternalResource:  conString,
				DesiredConfigHash: desiredClientConfigHash,
				SystemAttributes: &pb.SystemAttributes{
					SystemInMaintenanceMode: inMaintenanceMode,
					MirrorEnabled:           isConsumerMirrorEnabled,
				},
			},
			nil
	}

	return nil, status.Errorf(codes.Unavailable, "storage consumer status is not set")
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
	err = corev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add corev1 to scheme. %v", err)
	}
	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add rookCephv1 to scheme. %v", err)
	}
	err = opv1a1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add operatorsv1alpha1 to scheme. %v", err)
	}
	err = ocsv1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add ocsv1 to scheme. %v", err)
	}
	err = routev1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add routev1 to scheme. %v", err)
	}
	err = nbapis.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add nbapis to scheme. %v", err)
	}

	return scheme, nil
}

func (s *OCSProviderServer) getExternalResources(ctx context.Context, consumerResource *ocsv1alpha1.StorageConsumer) ([]*pb.ExternalResource, error) {
	var extR []*pb.ExternalResource

	// Configmap with mon endpoints
	configmap := &v1.ConfigMap{}
	err := s.client.Get(ctx, types.NamespacedName{Name: monConfigMap, Namespace: s.namespace}, configmap)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s configMap. %v", monConfigMap, err)
	}

	if configmap.Data["data"] == "" {
		return nil, fmt.Errorf("configmap %s data is empty", monConfigMap)
	}

	extR = append(extR, &pb.ExternalResource{
		Name: monConfigMap,
		Kind: "ConfigMap",
		Data: mustMarshal(map[string]string{
			"data":     configmap.Data["data"], // IP Address of all mon's
			"maxMonId": "0",
			"mapping":  "{}",
		})})

	monIps, err := extractMonitorIps(configmap.Data["data"])
	if err != nil {
		return nil, fmt.Errorf("failed to extract monitor IPs from configmap %s: %v", monConfigMap, err)
	}

	extR = append(extR, &pb.ExternalResource{
		Kind: "CephConnection",
		Name: "monitor-endpoints",
		Data: mustMarshal(&csiopv1a1.CephConnectionSpec{Monitors: monIps}),
	})

	scMon := &v1.Secret{}
	// Secret storing cluster mon.admin key, fsid and name
	err = s.client.Get(ctx, types.NamespacedName{Name: monSecret, Namespace: s.namespace}, scMon)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s secret. %v", monSecret, err)
	}

	fsid := string(scMon.Data["fsid"])
	if fsid == "" {
		return nil, fmt.Errorf("secret %s data fsid is empty", monSecret)
	}

	// Get mgr pod hostIP
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(s.namespace),
		client.MatchingLabels(map[string]string{"app": "rook-ceph-mgr"}),
	}
	err = s.client.List(ctx, podList, listOpts...)
	if err != nil {
		return nil, fmt.Errorf("failed to list pod with rook-ceph-mgr label. %v", err)
	}
	if len(podList.Items) == 0 {
		return nil, fmt.Errorf("no pods available with rook-ceph-mgr label")
	}

	mgrPod := &podList.Items[0]
	var port int32 = -1

	for i := range mgrPod.Spec.Containers {
		container := &mgrPod.Spec.Containers[i]
		if container.Name == "mgr" {
			for j := range container.Ports {
				if container.Ports[j].Name == "http-metrics" {
					port = container.Ports[j].ContainerPort
				}
			}
		}
	}

	if port < 0 {
		return nil, fmt.Errorf("mgr pod port is empty")
	}

	extR = append(extR, &pb.ExternalResource{
		Name: "monitoring-endpoint",
		Kind: "CephCluster",
		Data: mustMarshal(map[string]string{
			"MonitoringEndpoint": mgrPod.Status.HostIP,
			"MonitoringPort":     strconv.Itoa(int(port)),
		})})

	if consumerResource.Spec.StorageQuotaInGiB > 0 {
		clusterResourceQuotaSpec := &quotav1.ClusterResourceQuotaSpec{
			Selector: quotav1.ClusterResourceQuotaSelector{
				LabelSelector: &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{
						{
							Key:      string(consumerResource.UID),
							Operator: metav1.LabelSelectorOpDoesNotExist,
						},
					},
				},
			},
			Quota: corev1.ResourceQuotaSpec{
				Hard: corev1.ResourceList{"requests.storage": *resource.NewQuantity(
					int64(consumerResource.Spec.StorageQuotaInGiB)*oneGibInBytes,
					resource.BinarySI,
				)},
			},
		}

		extR = append(extR, &pb.ExternalResource{
			Name: "QuotaForConsumer",
			Kind: "ClusterResourceQuota",
			Data: mustMarshal(clusterResourceQuotaSpec),
		})

	}

	cbpList := &rookCephv1.CephBlockPoolList{}
	err = s.client.List(ctx, cbpList, client.InNamespace(s.namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list cephBlockPools in namespace. %v", err)
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
		clientName := consumerResource.Status.Client.Name
		clientProfileName := util.CalculateMD5Hash(fmt.Sprintf("%s-ceph-rbd", clientName))
		extR = append(extR, &pb.ExternalResource{
			Name: consumerResource.Status.Client.Name,
			Kind: "ClientProfileMapping",
			Data: mustMarshal(&csiopv1a1.ClientProfileMappingSpec{
				Mappings: []csiopv1a1.MappingsSpec{
					{
						LocalClientProfile:  clientProfileName,
						RemoteClientProfile: clientProfileName,
						BlockPoolIdMapping:  blockPoolMapping,
					},
				},
			}),
		})
	}

	// Fetch noobaa remote secret and management address and append to extResources
	consumerName := consumerResource.Name
	noobaaOperatorSecret := &v1.Secret{}
	noobaaOperatorSecret.Name = fmt.Sprintf("noobaa-account-%s", consumerName)
	noobaaOperatorSecret.Namespace = s.namespace

	if err := s.client.Get(ctx, client.ObjectKeyFromObject(noobaaOperatorSecret), noobaaOperatorSecret); err != nil {
		if kerrors.IsNotFound(err) {
			// ignoring because it is a provider cluster and the noobaa secret does not exist
			return extR, nil

		}
		return nil, fmt.Errorf("failed to get %s secret. %v", noobaaOperatorSecret.Name, err)
	}

	authToken, ok := noobaaOperatorSecret.Data["auth_token"]
	if !ok || len(authToken) == 0 {
		return nil, fmt.Errorf("auth_token not found in %s secret", noobaaOperatorSecret.Name)
	}

	noobaMgmtRoute := &routev1.Route{}
	noobaMgmtRoute.Name = "noobaa-mgmt"
	noobaMgmtRoute.Namespace = s.namespace

	if err = s.client.Get(ctx, client.ObjectKeyFromObject(noobaMgmtRoute), noobaMgmtRoute); err != nil {
		return nil, fmt.Errorf("failed to get noobaa-mgmt route. %v", err)
	}
	if len(noobaMgmtRoute.Status.Ingress) == 0 {
		return nil, fmt.Errorf("no Ingress available in noobaa-mgmt route")
	}

	noobaaMgmtAddress := noobaMgmtRoute.Status.Ingress[0].Host
	if noobaaMgmtAddress == "" {
		return nil, fmt.Errorf("no Host found in noobaa-mgmt route Ingress")
	}
	extR = append(extR, &pb.ExternalResource{
		Name: "noobaa-remote-join-secret",
		Kind: "Secret",
		Data: mustMarshal(map[string]string{
			"auth_token": string(authToken),
			"mgmt_addr":  noobaaMgmtAddress,
		}),
	})

	extR = append(extR, &pb.ExternalResource{
		Name: "noobaa-remote",
		Kind: "Noobaa",
		Data: mustMarshal(&nbv1.NooBaaSpec{
			JoinSecret: &v1.SecretReference{
				Name: "noobaa-remote-join-secret",
			},
		}),
	})

	nb := &nbv1.NooBaa{}
	nb.Name = "noobaa"
	nb.Namespace = s.namespace

	if err = s.client.Get(ctx, client.ObjectKeyFromObject(nb), nb); err != nil {
		return nil, fmt.Errorf("failed to get noobaa %v", err)
	}
	if nb.Status.Phase != nbv1.SystemPhaseReady {
		// Noobaa system is not yet ready
		return extR, nil
	}
	nbS3Endpoint := nb.Status.Services.ServiceS3.NodePorts[0]
	extR = append(extR, &pb.ExternalResource{
		Name: "s3-endpoint-proxy",
		Kind: "Service",
		Data: mustMarshal(&v1.ServiceSpec{
			Type:         corev1.ServiceTypeExternalName,
			ExternalName: nbS3Endpoint,
			Ports: []v1.ServicePort{
				{
					Port:       443,
					TargetPort: intstr.FromInt(443),
				},
			},
		},
		),
	})
	// TODO: configure S3 certificates
	return extR, nil
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
	case services.ClientRole:
		if ticketData.StorageQuotaInGiB != nil {
			quota := *ticketData.StorageQuotaInGiB
			if quota > math.MaxInt {
				return nil, fmt.Errorf("invalid value sent in onboarding ticket, storage quota should be greater than 0 and less than %v: %v", math.MaxInt, quota)
			}
		}
	case services.PeerRole:
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
				strconv.Itoa(blockPool.Status.PoolID),
				rns.Name,
			)

			peerPoolID := blockPool.GetAnnotations()[util.BlockPoolMirroringTargetIDAnnotation]
			radosNamespace := cmp.Or(rns.Spec.Mirroring, &rookCephv1.RadosNamespaceMirroring{}).RemoteNamespace
			if peerPoolID != "" && radosNamespace != nil {
				peerCephfsid, err := getPeerCephFSID(
					ctx,
					s.client,
					mirroring.GetMirroringSecretName(blockPool.Name),
					blockPool.Namespace,
				)
				if err != nil {
					klog.Errorf("failed to get peer Ceph FSIS. %v", err)
				}

				peerStorageID := calculateCephRbdStorageID(
					peerCephfsid,
					peerPoolID,
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
				subVolumeGroup.Spec.FilesystemName,
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

	channelName, err := s.getOCSSubscriptionChannel(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "Failed to construct status response: %v", err)
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

		owner := util.FindOwnerRefByKind(consumer, "StorageCluster")
		if owner == nil {
			klog.Infof("no owner found for consumer %v", req.ClientIDs[i])
			continue
		}

		if owner.UID != types.UID(req.StorageClusterUID) {
			klog.Infof("storageCluster specified on the req does not own the client %v", req.ClientIDs[i])
			continue
		}

		rnsList := &rookCephv1.CephBlockPoolRadosNamespaceList{}
		err = s.client.List(
			ctx,
			rnsList,
			client.InNamespace(s.namespace),
			client.MatchingLabels{controllers.StorageConsumerNameLabel: consumer.Name},
			client.Limit(2),
		)
		if err != nil {
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
		if len(rnsList.Items) > 1 {
			response.Errors = append(response.Errors,
				&pb.StorageClientInfoError{
					ClientID: req.ClientIDs[i],
					Code:     pb.ErrorCode_Internal,
					Message:  "failed loading client information",
				},
			)
			klog.Errorf("invalid number of radosnamespace found for the Client %v", req.ClientIDs[i])
			continue
		}
		clientInfo := &pb.ClientInfo{ClientID: req.ClientIDs[i]}
		if len(rnsList.Items) == 1 {
			clientInfo.RadosNamespace = rnsList.Items[0].Name
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

func calculateCephRbdStorageID(cephfsid, poolID, radosnamespacename string) string {
	return util.CalculateMD5Hash([3]string{cephfsid, poolID, radosnamespacename})
}

func calculateCephFsStorageID(cephfsid, fileSystemName, subVolumeGroupName string) string {
	return util.CalculateMD5Hash([3]string{cephfsid, fileSystemName, subVolumeGroupName})
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
