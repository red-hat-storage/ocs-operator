package server

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type storageClusterPeerManager struct {
	client    client.Client
	namespace string
}

func newStorageClusterPeerManager(cl client.Client, namespace string) *storageClusterPeerManager {
	return &storageClusterPeerManager{
		client:    cl,
		namespace: namespace,
	}
}

func (s *storageClusterPeerManager) GetByPeerStorageClusterUID(ctx context.Context, storageClusterUID types.UID) (*ocsv1.StorageClusterPeer, error) {
	storageClusterPeerList := &ocsv1.StorageClusterPeerList{}
	err := s.client.List(ctx, storageClusterPeerList, client.InNamespace(s.namespace))
	if err != nil {
		return nil, fmt.Errorf("failed to list StorageClusterPeer: %v", err)
	}
	for i := range storageClusterPeerList.Items {
		scp := &storageClusterPeerList.Items[i]
		if scp.Status.PeerInfo != nil && types.UID(scp.Status.PeerInfo.StorageClusterUid) == storageClusterUID {
			return scp, nil
		}
	}
	return nil, fmt.Errorf("StorageClusterPeer linked to StorageCluster with uid %q not found", storageClusterUID)
}
