package server

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/types"
	klog "k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	annotationKeyRemoteObcCreation     = "remote-obc-creation"
	labelKeyRemoteObcOriginalName      = "remote-obc-original-name"
	labelKeyRemoteObcOriginalNamespace = "remote-obc-original-namespace"
	labelKeyRemoteObcConsumerName      = "storage-consumer-name"
	labelKeyRemoteObcConsumerUUID      = "storage-consumer-uuid"
	prefixOfHashedName                 = "remote-obc"
)

// handleObcCreated create the OBC that the client cluster asked for on the provider cluster.
// It is a synchronous call, we do not wait for resources to be created.
// Notes:
//   - OBC is created in the storage consumer namespace (and not the provider server namespace in case it would be moved)
//   - The OBC is named with an obscure name to avoid collisions
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

	storageConsumer, err := s.consumerManager.Get(ctx, storageConsumerUUID)
	if err != nil {
		return status.Errorf(codes.Internal, "OBC cannot be created due to failed StorageConsumer lookup: storageConsumerUUID=%s", storageConsumerUUID)
	}
	storageConsumerName := storageConsumer.Name

	obcNameClient := obcDetails.Name
	namespaceNameClient := obcDetails.Namespace
	if obcNameClient == "" || namespaceNameClient == "" {
		return status.Error(codes.InvalidArgument, "missing OBC name or namespace")
	}

	logger.Info("handleObcCreate: Building OBC object", "storageConsumerUUID", storageConsumerUUID, "obcNameClient", obcNameClient, "namespaceNameClient", namespaceNameClient)
	obc := &nbv1.ObjectBucketClaim{}
	obc.Name = getObcHashedName(storageConsumerUUID, obcNameClient, namespaceNameClient)
	obc.Namespace = storageConsumer.Namespace

	util.AddLabel(obc, labelKeyRemoteObcConsumerName, storageConsumerName)
	util.AddLabel(obc, labelKeyRemoteObcConsumerUUID, storageConsumerUUID)
	util.AddLabel(obc, labelKeyRemoteObcOriginalName, obcNameClient)
	util.AddLabel(obc, labelKeyRemoteObcOriginalNamespace, namespaceNameClient)

	util.AddAnnotation(obc, annotationKeyRemoteObcCreation, "true")

	obc.Spec = obcDetails.Spec

	if err := controllerutil.SetOwnerReference(storageConsumer, obc, s.scheme); err != nil {
		return status.Errorf(codes.Internal, "failed to set owner reference for OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}

	logger.Info("handleObcCreate: Creating OBC resource", "namespaced/name", client.ObjectKeyFromObject(obc))
	if err := s.client.Create(ctx, obc); client.IgnoreAlreadyExists(err) != nil {
		return status.Errorf(codes.Internal, "failed to create OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	return nil
}

// handleObcDeleted delete the OBC that the client cluster asked for on the provider cluster.
// It is a synchronous call, we do not wait for resources to be deleted.
// Notes:
//   - OBC is deleted from the storage consumer namespace using the labels set during creation.
func (s *OCSProviderServer) handleObcDeleted(ctx context.Context, storageConsumerUUID string, obcDetails types.NamespacedName) error {
	logger := klog.FromContext(ctx).WithName("handleObcDelete")
	logger.Info("handleObcDelete: Starting handleObcDelete", "storageConsumerUUID", storageConsumerUUID)

	storageConsumer, err := s.consumerManager.Get(ctx, storageConsumerUUID)
	if err != nil {
		return status.Errorf(codes.Internal, "OBC cannot be deleted due to failed StorageConsumer lookup: storageConsumerUUID=%s", storageConsumerUUID)
	}
	storageConsumerName := storageConsumer.Name

	obcNameClient := obcDetails.Name
	namespaceNameClient := obcDetails.Namespace
	if obcNameClient == "" || namespaceNameClient == "" {
		logger.Error(nil, "handleObcDelete: missing OBC name or namespace", obcDetails)
		return status.Error(codes.InvalidArgument, "missing OBC name or namespace")
	}

	labelSelector := map[string]string{
		labelKeyRemoteObcOriginalName:      obcNameClient,
		labelKeyRemoteObcOriginalNamespace: namespaceNameClient,
		labelKeyRemoteObcConsumerName:      storageConsumerName,
	}
	namespaceObc := storageConsumer.Namespace
	obcList := &nbv1.ObjectBucketClaimList{}
	if err := s.client.List(ctx, obcList, client.InNamespace(namespaceObc), client.MatchingLabels(labelSelector)); err != nil {
		logger.Error(err, "handleObcDelete: Failed to list OBC resources", "namespace", namespaceObc, "labels", labelSelector)
		return status.Errorf(codes.Internal, "failed to list OBCs for deletion name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	if len(obcList.Items) == 0 {
		logger.Info("handleObcDelete: OBC not found", "namespace", namespaceObc, "labels", labelSelector)
		return nil
	}
	if len(obcList.Items) > 1 {
		logger.Error(nil, "handleObcDelete: Multiple OBCs matched labels", "namespace", namespaceObc, "labels", labelSelector, "count", len(obcList.Items))
		return status.Errorf(codes.Internal, "multiple OBCs matched for deletion name %s namespace %s", obcNameClient, namespaceNameClient)
	}

	obc := &obcList.Items[0]
	logger.Info("handleObcDelete: Deleting OBC resource", "namespaced/name", client.ObjectKeyFromObject(obc))
	if err := s.client.Delete(ctx, obc); client.IgnoreNotFound(err) != nil {
		return status.Errorf(codes.Internal, "failed to delete OBC name %s namespace %s: %v", obcNameClient, namespaceNameClient, err)
	}
	return nil
}

// getObcHashedName creates a stable hash for OBC name
// obcName and obcNamespace are from the client cluster
// this function is based on getStorageRequestHash function
func getObcHashedName(storageConsumerUUID, obcName, obcNamespace string) string {
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
		panic("failed to marshal obc hash")
	}
	md5Sum := md5.Sum(obcHash)
	hashString := hex.EncodeToString(md5Sum[:16])
	return fmt.Sprintf("%s-%s", prefixOfHashedName, hashString)
}
