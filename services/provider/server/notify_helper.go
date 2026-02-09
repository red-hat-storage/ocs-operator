package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	annotationKeyRemoteObcCreation     = "remote-obc-creation"
	labelKeyRemoteObcOriginalName      = "remote-obc-original-name"
	labelKeyRemoteObcOriginalNamespace = "remote-obc-original-namespace"
	labelKeyRemoreObcConsumerName      = "remote-obc-consumer-name"
	labelKeyRemoreObcConsumerUUID      = "remote-obc-consumer-uuid"
	prefixOfHashedName                 = "remote-obc"
)

// handleObcCreated create the OBC that the client cluster asked for on the provider cluster.
// It is a synchronous call, we do not wait for resources to be created.
// Notes:
//   - OBC is created in the provider server namespace with an obscure name to avoid collisions
//   - Owner reference is set to the storage consumer
//   - Label added: original indicates that the information is about the client cluster OBC
//     1. "remote-obc-original-name": "<original-obc-name>"       // name provided by the client
//     2. "remote-obc-original-namespace": "<original-obc-namespace>"    // namespace provided by the client
//     3. "remote-obc-consumer-name": "<consumer-name>" // name of the storage consumer
//     4. "remote-obc-consumer-uuid": "<consumer-uuid>" // UUID of the storage consumer
//   - Annotations added:
//     1. "remote-obc-creation": "true" // used by MCG CLI
func (s *OCSProviderServer) handleObcCreated(ctx context.Context, storageConsumerUUID string, obcDetails *nbv1.ObjectBucketClaim) error {
	logger := klog.FromContext(ctx).WithName("handleObcCreate")
	logger.Info("handleObcCreate: Starting handleObcCreate", "storageConsumerUUID", storageConsumerUUID)

	// (1) get the storage consumer
	storageConsumer, err := s.consumerManager.Get(ctx, storageConsumerUUID)
	if err != nil {
		logger.Error(err, "handleObcCreate: failed to get StorageConsumer", "storageConsumerUUID", storageConsumerUUID)
		return status.Errorf(codes.Internal, "OBC cannot be created due to failed StorageConsumer lookup: storageConsumerUUID=%s", storageConsumerUUID)
	}
	if storageConsumer == nil {
		return status.Errorf(codes.Internal, "OBC cannot be created due to missing StorageConsumer: storageConsumerUUID=%s", storageConsumerUUID)
	}
	storageConsumerName := storageConsumer.Name

	// (2) extract OBC details from payload
	if obcDetails == nil {
		return status.Error(codes.InvalidArgument, "missing OBC payload")
	}
	obcNameClient := obcDetails.Name
	namespaceNameClient := obcDetails.Namespace
	if obcNameClient == "" || namespaceNameClient == "" {
		return status.Error(codes.InvalidArgument, "missing OBC name or namespace in payload")
	}

	// (3) build OBC object
	logger.Info("handleObcCreate: Building OBC object", "storageConsumerUUID", storageConsumerUUID, "obcNameClient", obcNameClient, "namespaceNameClient", namespaceNameClient)
	obc := &nbv1.ObjectBucketClaim{}
	// set resource name to a hash based on storage consumer UUID, OBC name, and OBC namespace
	obc.Name = getObcHashedName(storageConsumerUUID, obcNameClient, namespaceNameClient)
	// create in the namespace associated with the provider server
	obc.Namespace = s.namespace

	// add labels for the searching of OBC
	if obc.Labels == nil {
		obc.Labels = map[string]string{}
	}
	obc.Labels[labelKeyRemoreObcConsumerName] = storageConsumerName
	obc.Labels[labelKeyRemoreObcConsumerUUID] = storageConsumerUUID
	obc.Labels[labelKeyRemoteObcOriginalName] = obcNameClient
	obc.Labels[labelKeyRemoteObcOriginalNamespace] = namespaceNameClient

	// add annotations indicating remote OBC creation and store original details
	if obc.Annotations == nil {
		obc.Annotations = map[string]string{}
	}
	obc.Annotations[annotationKeyRemoteObcCreation] = "true" // used in mcg-cli

	// populate fields in the OBC spec from payload
	obc.Spec = obcDetails.Spec

	// set owner reference based on storage consumer
	if err := controllerutil.SetOwnerReference(storageConsumer, obc, s.scheme); err != nil {
		logger.Error(err, "handleObcCreate: Failed to set owner reference", "storageConsumer", storageConsumer.Name)
		return status.Errorf(codes.Internal, "failed to set owner reference for OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	logger.Info("handleObcCreate: Added owner reference to OBC", "owner", storageConsumer.Name)

	// (4) create the OBC resource
	logger.Info("handleObcCreate: Creating OBC resource", "name", obc.Name, "namespace", obc.Namespace)
	if err := s.client.Create(ctx, obc); err != nil {
		if errors.IsAlreadyExists(err) {
			return status.Errorf(codes.AlreadyExists, "OBC already exists name %s namespace %s", obcNameClient, namespaceNameClient)
		}
		logger.Error(err, "handleObcCreate: Failed to create OBC resource:", "storageConsumerUUID", storageConsumerUUID, "storageConsumerName", storageConsumerName, "name", obc.Name, "namespace", obc.Namespace)
		return status.Errorf(codes.Internal, "failed to create OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	logger.Info("handleObcCreate: Successfully created OBC resource", "name", obc.Name, "namespace", obc.Namespace)
	return nil
}

// handleObcDeleted delete the OBC that the client cluster asked for on the provider cluster.
// It is a synchronous call, we do not wait for resources to be deleted.
// Notes:
//   - OBC is deleted from the provider server namespace using the labels set during creation.
func (s *OCSProviderServer) handleObcDeleted(ctx context.Context, storageConsumerUUID string, payload types.NamespacedName) error {
	logger := klog.FromContext(ctx).WithName("handleObcDelete")
	logger.Info("handleObcDelete: Starting handleObcDelete", "storageConsumerUUID", storageConsumerUUID)

	// (1) get the storage consumer
	storageConsumer, err := s.consumerManager.Get(ctx, storageConsumerUUID)
	if err != nil {
		logger.Error(err, "handleObcDelete: failed to get StorageConsumer", "storageConsumerUUID", storageConsumerUUID)
		return status.Errorf(codes.Internal, "OBC cannot be deleted due to failed StorageConsumer lookup: storageConsumerUUID=%s", storageConsumerUUID)
	}
	if storageConsumer == nil {
		return status.Errorf(codes.Internal, "OBC cannot be deleted due to missing StorageConsumer: storageConsumerUUID=%s", storageConsumerUUID)
	}
	storageConsumerName := storageConsumer.Name

	// (2) extract OBC details from payload
	obcNameClient := payload.Name
	namespaceNameClient := payload.Namespace
	if obcNameClient == "" || namespaceNameClient == "" {
		return status.Error(codes.InvalidArgument, "missing OBC name or namespace in payload")
	}

	// (3) find OBC by labels (AND logic between the labels)
	labelSelector := map[string]string{
		labelKeyRemoteObcOriginalName:      obcNameClient,
		labelKeyRemoteObcOriginalNamespace: namespaceNameClient,
		labelKeyRemoreObcConsumerName:      storageConsumerName,
	}
	obcList := &nbv1.ObjectBucketClaimList{}
	if err := s.client.List(ctx, obcList, client.InNamespace(s.namespace), client.MatchingLabels(labelSelector)); err != nil {
		logger.Error(err, "handleObcDelete: Failed to list OBC resources", "namespace", s.namespace, "labels", labelSelector)
		return status.Errorf(codes.Internal, "failed to list OBCs for deletion name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	if len(obcList.Items) == 0 {
		logger.Info("handleObcDelete: OBC not found", "namespace", s.namespace, "labels", labelSelector)
		return status.Errorf(codes.NotFound, "OBC not found name %s namespace %s", obcNameClient, namespaceNameClient)
	}
	if len(obcList.Items) > 1 {
		logger.Error(nil, "handleObcDelete: Multiple OBCs matched labels", "namespace", s.namespace, "labels", labelSelector, "count", len(obcList.Items))
		return status.Errorf(codes.Internal, "multiple OBCs matched for deletion name %s namespace %s", obcNameClient, namespaceNameClient)
	}

	// (4) delete the OBC
	obc := &obcList.Items[0]
	logger.Info("handleObcDelete: Deleting OBC resource", "name", obc.Name, "namespace", s.namespace, "labels", labelSelector)
	if err := s.client.Delete(ctx, obc); err != nil {
		if errors.IsNotFound(err) {
			return status.Errorf(codes.NotFound, "OBC not found name %s namespace %s", obcNameClient, namespaceNameClient)
		}
		logger.Error(err, "handleObcDelete: Failed to delete OBC resource", "name", obc.Name, "namespace", s.namespace)
		return status.Errorf(codes.Internal, "failed to delete OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	logger.Info("handleObcDelete: Successfully deleted OBC resource", "name", obc.Name, "namespace", s.namespace)
	return nil
}

// getObcHash creates a stable hash for naming the created OBC resource.
// this function is based on getStorageRequestHash function
func getObcHash(storageConsumerUUID, obcName, obcNamespace string) string {
	s := struct {
		StorageConsumerUUID string `json:"storageConsumerUUID"`
		ObcName             string `json:"obcName"`
		ObcNamespace        string `json:"obcNamespace"`
	}{
		storageConsumerUUID,
		obcName,
		obcNamespace,
	}
	obcHash, err := json.Marshal(s)
	if err != nil {
		panic("failed to marshal obc hash payload")
	}
	md5Sum := md5.Sum(obcHash)
	return hex.EncodeToString(md5Sum[:16])
}

// getObcHashedName creates a stable hash for OBC name
// obcName and obcNamespace are from the client cluster
func getObcHashedName(storageConsumerUUID, obcName, obcNamespace string) string {
	hash := getObcHash(storageConsumerUUID, obcName, obcNamespace)
	return fmt.Sprintf("%s-%s", prefixOfHashedName, hash)
}
