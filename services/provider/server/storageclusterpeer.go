package server

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type storageClusterPeerManager struct {
	client    client.Client
	namespace string
}

func newStorageClusterPeerManager(cl client.Client, namespace string) (*storageClusterPeerManager, error) {
	return &storageClusterPeerManager{
		client:    cl,
		namespace: namespace,
	}, nil
}

func (s *storageClusterPeerManager) FindStorageClusterPeerWithStorageClusterID(ctx context.Context, storageClusterUID string) (*ocsv1.StorageClusterPeer, error) {
	storageClusterPeerList := &ocsv1.StorageClusterPeerList{}
	err := s.client.List(ctx, storageClusterPeerList, client.InNamespace(s.namespace))
	if err != nil {
		return nil, err
	}
	for i := range storageClusterPeerList.Items {
		token, err := decodeToken(storageClusterPeerList.Items[i].Spec.OnboardingToken)
		if err != nil {
			return nil, err
		}
		if token.StorageClusterUID == storageClusterUID {
			return &storageClusterPeerList.Items[i], nil
		}
	}
	return nil, fmt.Errorf("StorageClusterPeer linked to StorageCluster with uid %q not found", storageClusterUID)
}

func (s *storageClusterPeerManager) UpdateStorageClusterPeerStatus(ctx context.Context, storageClusterPeer *ocsv1.StorageClusterPeer, storageClusterUID string) error {

	storageClusterPeer.Status.RemoteStorageClusterUID = storageClusterUID

	if err := s.client.Status().Update(ctx, storageClusterPeer); err != nil {
		return fmt.Errorf("failed to patch Status for StorageClusterPeer %v: %v", storageClusterPeer.Name, err)
	}
	klog.Infof("successfully updated Status for StorageConsumer %v", storageClusterPeer.Name)
	return nil
}
