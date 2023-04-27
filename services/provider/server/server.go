package server

import (
	"context"
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"
	pb "github.com/red-hat-storage/ocs-operator/services/provider/pb"
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
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

const (
	TicketAnnotation             = "ocs.openshift.io/provider-onboarding-ticket"
	ProviderCertsMountPoint      = "/mnt/cert"
	onboardingTicketKeySecret    = "onboarding-ticket-key"
	storageClassRequestNameLabel = "ocs.openshift.io/storageclassrequest-name"
)

const (
	monConfigMap = "rook-ceph-mon-endpoints"
	monSecret    = "rook-ceph-mon"
)

type OCSProviderServer struct {
	pb.UnimplementedOCSProviderServer
	client                     client.Client
	consumerManager            *ocsConsumerManager
	storageClassRequestManager *storageClassRequestManager
	namespace                  string
}

type onboardingTicket struct {
	ID             string `json:"id"`
	ExpirationDate int64  `json:"expirationDate,string"`
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

	storageClassRequestManager, err := newStorageClassRequestManager(ctx, client, namespace)
	if err != nil {
		return nil, fmt.Errorf("failed to create new StorageClassRequest instance. %v", err)
	}

	return &OCSProviderServer{
		client:                     client,
		consumerManager:            consumerManager,
		storageClassRequestManager: storageClassRequestManager,
		namespace:                  namespace,
	}, nil
}

// OnboardConsumer RPC call to onboard a new OCS consumer cluster.
func (s *OCSProviderServer) OnboardConsumer(ctx context.Context, req *pb.OnboardConsumerRequest) (*pb.OnboardConsumerResponse, error) {

	// Validate capacity
	capacity, err := resource.ParseQuantity(req.Capacity)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%q is not a valid storageConsumer capacity: %v", req.Capacity, err)
	}

	pubKey, err := s.getOnboardingValidationKey(ctx)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "failed to get public key to validate onboarding ticket for consumer %q. %v", req.ConsumerName, err)
	}

	if err := validateTicket(req.OnboardingTicket, pubKey); err != nil {
		klog.Errorf("failed to validate onboarding ticket for consumer %q. %v", req.ConsumerName, err)
		return nil, status.Errorf(codes.InvalidArgument, "onboarding ticket is not valid. %v", err)
	}

	storageConsumerUUID, err := s.consumerManager.Create(ctx, req.ConsumerName, req.OnboardingTicket, capacity)
	if err != nil {
		if !kerrors.IsAlreadyExists(err) && err != errTicketAlreadyExists {
			return nil, status.Errorf(codes.Internal, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
		}

		stoageConsumer, err := s.consumerManager.GetByName(ctx, req.ConsumerName)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "Failed to get storageConsumer. %v", err)
		}

		if stoageConsumer.Spec.Enable {
			err = fmt.Errorf("storageconsumers.ocs.openshift.io %s already exists", req.ConsumerName)
			return nil, status.Errorf(codes.AlreadyExists, "failed to create storageConsumer %q. %v", req.ConsumerName, err)
		}
		storageConsumerUUID = string(stoageConsumer.UID)
	}

	// TODO: send correct granted capacity
	return &pb.OnboardConsumerResponse{StorageConsumerUUID: storageConsumerUUID, GrantedCapacity: req.Capacity}, nil
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
		klog.Infof("successfully returned the config details to the consumer.")
		return &pb.StorageConfigResponse{ExternalResource: conString}, nil
	}

	return nil, status.Errorf(codes.Unavailable, "storage consumer status is not set")
}

// UpdateCapacity PRC call to increase or decrease the storage pool size
func (s *OCSProviderServer) UpdateCapacity(ctx context.Context, req *pb.UpdateCapacityRequest) (*pb.UpdateCapacityResponse, error) {

	capacity, err := resource.ParseQuantity(req.Capacity)
	if err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "%q is not a valid resource capacity: %v", req.Capacity, err)
	}

	if err := s.consumerManager.UpdateCapacity(ctx, req.StorageConsumerUUID, capacity); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "failed to update capacity in the storageConsumer resource: %v", err)
		} else if err == errFailedPrecondition {
			return nil, status.Errorf(codes.FailedPrecondition, "failed to update capacity in the storageConsumer resource: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "failed to update capacity in the storageConsumer resource: %v", err)
	}

	return &pb.UpdateCapacityResponse{GrantedCapacity: req.Capacity}, nil
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
	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		return nil, fmt.Errorf("failed to add rookCephv1 to scheme. %v", err)
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

	healthCheckerSecretName := ""
	healthCheckerName := ""
	for _, cephRes := range consumerResource.Status.CephResources {
		if cephRes.Kind == "CephClient" {
			clientSecretName, cephUserType, err := s.getCephClientInformation(ctx, cephRes.Name)
			if err != nil {
				return nil, err
			} else if cephUserType == "healthchecker" {
				healthCheckerSecretName = clientSecretName
				healthCheckerName = cephRes.Name
				break
			}
		}
	}

	if healthCheckerSecretName == "" {
		return nil, fmt.Errorf("no healthchecker secret found")
	}

	cephUserSecret := &v1.Secret{}
	err = s.client.Get(ctx, types.NamespacedName{Name: healthCheckerSecretName, Namespace: s.namespace}, cephUserSecret)
	if err != nil {
		return nil, fmt.Errorf("failed to get %s secret. %v", healthCheckerSecretName, err)
	}

	extR = append(extR, &pb.ExternalResource{
		Name: healthCheckerSecretName,
		Kind: "Secret",
		Data: mustMarshal(map[string]string{
			"userID":  healthCheckerName,
			"userKey": string(cephUserSecret.Data[healthCheckerName]),
		}),
	})

	extR = append(extR, &pb.ExternalResource{
		Name: monSecret,
		Kind: "Secret",
		Data: mustMarshal(map[string]string{
			"fsid":          fsid,
			"mon-secret":    "mon-secret",
			"ceph-username": fmt.Sprintf("client.%s", healthCheckerName),
			"ceph-secret":   string(cephUserSecret.Data[healthCheckerName]),
		}),
	})

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

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("failed to parse public key. %v", err)
	}

	return key.(*rsa.PublicKey), nil
}

func mustMarshal(data map[string]string) []byte {
	newData, err := json.Marshal(data)
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

func validateTicket(ticket string, pubKey *rsa.PublicKey) error {
	ticketArr := strings.Split(string(ticket), ".")
	if len(ticketArr) != 2 {
		return fmt.Errorf("invalid ticket")
	}

	message, err := base64.StdEncoding.DecodeString(ticketArr[0])
	if err != nil {
		return fmt.Errorf("failed to decode onboarding ticket: %v", err)
	}

	var ticketData onboardingTicket
	err = json.Unmarshal(message, &ticketData)
	if err != nil {
		return fmt.Errorf("failed to unmarshal onboarding ticket message. %v", err)
	}

	signature, err := base64.StdEncoding.DecodeString(ticketArr[1])
	if err != nil {
		return fmt.Errorf("failed to decode onboarding ticket %s signature: %v", ticketData.ID, err)
	}

	hash := sha256.Sum256(message)
	err = rsa.VerifyPKCS1v15(pubKey, crypto.SHA256, hash[:], signature)
	if err != nil {
		return fmt.Errorf("failed to verify onboarding ticket signature. %v", err)
	}

	if ticketData.ExpirationDate < time.Now().Unix() {
		return fmt.Errorf("onboarding ticket %s is expired", ticketData.ID)
	}

	klog.Infof("onboarding ticket %s has been verified successfully", ticketData.ID)

	return nil
}

// FulfillStorageClassClaim RPC call to create the StorageclassClaim CR on
// provider cluster.
func (s *OCSProviderServer) FulfillStorageClassClaim(ctx context.Context, req *pb.FulfillStorageClassClaimRequest) (*pb.FulfillStorageClassClaimResponse, error) {
	// Get storage consumer resource using UUID
	consumerObj, err := s.consumerManager.Get(ctx, req.StorageConsumerUUID)
	if err != nil {
		return nil, status.Errorf(codes.Internal, err.Error())
	}

	klog.Infof("Found StorageConsumer %q (%q)", consumerObj.Name, req.StorageConsumerUUID)

	var storageType string
	switch req.StorageType {
	case pb.FulfillStorageClassClaimRequest_BLOCKPOOL:
		storageType = "blockpool"
	case pb.FulfillStorageClassClaimRequest_SHAREDFILESYSTEM:
		storageType = "sharedfilesystem"
	default:
		return nil, status.Errorf(codes.InvalidArgument, "encountered an unknown storage type, %s", storageType)
	}

	err = s.storageClassRequestManager.Create(ctx, consumerObj, req.StorageClassClaimName, storageType, req.EncryptionMethod, req.StorageProfile)
	if err != nil {
		errMsg := fmt.Sprintf("failed to fulfill storage class claim for %q. %v", req.StorageConsumerUUID, err)
		klog.Error(errMsg)
		if kerrors.IsAlreadyExists(err) {
			return nil, status.Error(codes.AlreadyExists, errMsg)
		}
		return nil, status.Error(codes.Internal, errMsg)
	}

	return &pb.FulfillStorageClassClaimResponse{}, nil
}

// RevokeStorageClassClaim RPC call to delete the StorageclassClaim CR on
// provider cluster.
func (s *OCSProviderServer) RevokeStorageClassClaim(ctx context.Context, req *pb.RevokeStorageClassClaimRequest) (*pb.RevokeStorageClassClaimResponse, error) {
	err := s.storageClassRequestManager.Delete(ctx, req.StorageConsumerUUID, req.StorageClassClaimName)
	if err != nil {
		errMsg := fmt.Sprintf("failed to revoke storage class claim %q for %q. %v", req.StorageClassClaimName, req.StorageConsumerUUID, err)
		klog.Error(errMsg)
		return nil, status.Errorf(codes.Internal, errMsg)
	}

	return &pb.RevokeStorageClassClaimResponse{}, nil
}

// GetStorageClassClaim RPC call to get the ceph resources for the StorageclassClaim.
func (s *OCSProviderServer) GetStorageClassClaimConfig(ctx context.Context, req *pb.StorageClassClaimConfigRequest) (*pb.StorageClassClaimConfigResponse, error) {
	storageClassRequest, err := s.storageClassRequestManager.Get(ctx, req.StorageConsumerUUID, req.StorageClassClaimName)
	if err != nil {
		errMsg := fmt.Sprintf("failed to get storage class claim config %q for %q. %v", req.StorageClassClaimName, req.StorageConsumerUUID, err)
		if kerrors.IsNotFound(err) {
			return nil, status.Error(codes.NotFound, errMsg)
		}
		return nil, status.Error(codes.Internal, errMsg)
	}

	// Verify Status.Phase
	msg := fmt.Sprintf("storage class claim %q for %q is in %q phase", req.StorageClassClaimName, req.StorageConsumerUUID, storageClassRequest.Status.Phase)
	klog.Info(msg)
	if storageClassRequest.Status.Phase != ocsv1alpha1.StorageClassRequestReady {
		switch storageClassRequest.Status.Phase {
		case ocsv1alpha1.StorageClassRequestFailed:
			return nil, status.Error(codes.Internal, msg)
		case ocsv1alpha1.StorageClassRequestInitializing:
			return nil, status.Error(codes.Unavailable, msg)
		case ocsv1alpha1.StorageClassRequestCreating:
			return nil, status.Error(codes.Unavailable, msg)
		case "":
			return nil, status.Errorf(codes.Unavailable, "status is not set for storage class claim %q for %q", req.StorageClassClaimName, req.StorageConsumerUUID)
		default:
			return nil, status.Error(codes.Internal, msg)
		}
	}
	var extR []*pb.ExternalResource
	for _, cephRes := range storageClassRequest.Status.CephResources {
		switch cephRes.Kind {
		case "CephClient":
			clientSecretName, _, err := s.getCephClientInformation(ctx, cephRes.Name)
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			cephUserSecret := &v1.Secret{}
			err = s.client.Get(ctx, types.NamespacedName{Name: clientSecretName, Namespace: s.namespace}, cephUserSecret)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s secret. %v", clientSecretName, err)
			}

			idProp := "userID"
			keyProp := "userKey"
			if storageClassRequest.Spec.Type == "sharedfilesystem" {
				idProp = "adminID"
				keyProp = "adminKey"
			}
			extR = append(extR, &pb.ExternalResource{
				Name: clientSecretName,
				Kind: "Secret",
				Data: mustMarshal(map[string]string{
					idProp:  cephRes.Name,
					keyProp: string(cephUserSecret.Data[cephRes.Name]),
				}),
			})

		case "CephBlockPool":
			nodeCephClientSecret, _, err := s.getCephClientInformation(ctx, cephRes.CephClients["node"])
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			provisionerCephClientSecret, _, err := s.getCephClientInformation(ctx, cephRes.CephClients["provisioner"])
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}
			rbdStorageClass := map[string]string{
				"clusterID":                 s.namespace,
				"pool":                      cephRes.Name,
				"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
				"csi.storage.k8s.io/fstype": "ext4",
				"imageFormat":               "2",
				"csi.storage.k8s.io/provisioner-secret-name":       provisionerCephClientSecret,
				"csi.storage.k8s.io/node-stage-secret-name":        nodeCephClientSecret,
				"csi.storage.k8s.io/controller-expand-secret-name": provisionerCephClientSecret,
			}
			if storageClassRequest.Spec.EncryptionMethod != "" {
				rbdStorageClass["encrypted"] = "true"
				rbdStorageClass["encryptionKMSID"] = storageClassRequest.Spec.EncryptionMethod
			}
			extR = append(extR, &pb.ExternalResource{
				Name: "ceph-rbd",
				Kind: "StorageClass",
				Data: mustMarshal(rbdStorageClass)})

			extR = append(extR, &pb.ExternalResource{
				Name: "ceph-rbd",
				Kind: "VolumeSnapshotClass",
				Data: mustMarshal(map[string]string{
					"clusterID": s.namespace,
					"csi.storage.k8s.io/snapshotter-secret-name": provisionerCephClientSecret,
				})})

		case "CephFilesystemSubVolumeGroup":
			subVolumeGroup := &rookCephv1.CephFilesystemSubVolumeGroup{}
			err := s.client.Get(ctx, types.NamespacedName{Name: cephRes.Name, Namespace: s.namespace}, subVolumeGroup)
			if err != nil {
				return nil, status.Errorf(codes.Internal, "failed to get %s cephFilesystemSubVolumeGroup. %v", cephRes.Name, err)
			}

			nodeCephClientSecret, _, err := s.getCephClientInformation(ctx, cephRes.CephClients["node"])
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			provisionerCephClientSecret, _, err := s.getCephClientInformation(ctx, cephRes.CephClients["provisioner"])
			if err != nil {
				return nil, status.Error(codes.Internal, err.Error())
			}

			extR = append(extR, &pb.ExternalResource{
				Name: "cephfs",
				Kind: "StorageClass",
				Data: mustMarshal(map[string]string{
					"clusterID":          getSubVolumeGroupClusterID(subVolumeGroup),
					"subvolumegroupname": subVolumeGroup.Name,
					"fsName":             subVolumeGroup.Spec.FilesystemName,
					"csi.storage.k8s.io/provisioner-secret-name":       provisionerCephClientSecret,
					"csi.storage.k8s.io/node-stage-secret-name":        nodeCephClientSecret,
					"csi.storage.k8s.io/controller-expand-secret-name": provisionerCephClientSecret,
					"pool": subVolumeGroup.GetLabels()[v1alpha1.CephFileSystemDataPoolLabel],
				})})

			extR = append(extR, &pb.ExternalResource{
				Name: cephRes.Name,
				Kind: cephRes.Kind,
				Data: mustMarshal(map[string]string{
					"filesystemName": subVolumeGroup.Spec.FilesystemName,
				})})

			extR = append(extR, &pb.ExternalResource{
				Name: "cephfs",
				Kind: "VolumeSnapshotClass",
				Data: mustMarshal(map[string]string{
					"clusterID": getSubVolumeGroupClusterID(subVolumeGroup),
					"csi.storage.k8s.io/snapshotter-secret-name": provisionerCephClientSecret,
				})})
		}
	}
	klog.Infof("successfully returned the storage class claim %q for %q", req.StorageClassClaimName, req.StorageConsumerUUID)
	return &pb.StorageClassClaimConfigResponse{ExternalResource: extR}, nil

}

// ReportStatus rpc call to check if a consumer can reach to the provider.
func (s *OCSProviderServer) ReportStatus(ctx context.Context, req *pb.ReportStatusRequest) (*pb.ReportStatusResponse, error) {
	// Update the status in storageConsumer CR
	if err := s.consumerManager.UpdateStatusLastHeatbeat(ctx, req.StorageConsumerUUID); err != nil {
		if kerrors.IsNotFound(err) {
			return nil, status.Errorf(codes.NotFound, "Failed to update lastHeartbeat in the storageConsumer resource: %v", err)
		}
		return nil, status.Errorf(codes.Internal, "Failed to update lastHeartbeat in the storageConsumer resource: %v", err)
	}

	return &pb.ReportStatusResponse{}, nil
}
