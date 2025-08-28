package server

import (
	"context"
	"testing"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"

	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

func createTestStorageClusterPeerManager(client client.Client) *storageClusterPeerManager {
	return &storageClusterPeerManager{
		client:    client,
		namespace: testNamespace,
	}
}

func TestGetByPeerStorageClusterUID(t *testing.T) {
	peer1 := &ocsv1.StorageClusterPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-peer-1",
			Namespace: testNamespace,
		},
		Status: ocsv1.StorageClusterPeerStatus{
			PeerInfo: &ocsv1.PeerInfo{
				StorageClusterUid: "storage-cluster-uid-123",
			},
		},
	}

	peerDifferentNS := &ocsv1.StorageClusterPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-peer-different-ns",
			Namespace: "different-namespace",
		},
		Status: ocsv1.StorageClusterPeerStatus{
			PeerInfo: &ocsv1.PeerInfo{
				StorageClusterUid: "storage-cluster-uid-123",
			},
		},
	}

	peerWithNilInfo := &ocsv1.StorageClusterPeer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "test-peer-nil",
			Namespace: testNamespace,
		},
		Status: ocsv1.StorageClusterPeerStatus{
			State:    ocsv1.StorageClusterPeerStatePending,
			PeerInfo: nil,
		},
	}

	tests := []struct {
		name              string
		storageClusterUID string
		existingObjs      []client.Object
		expectError       bool
		expectedName      string
	}{
		{
			name:              "successful get by storage cluster UID",
			storageClusterUID: "storage-cluster-uid-123",
			existingObjs:      []client.Object{peer1},
			expectError:       false,
			expectedName:      "test-peer-1",
		},
		{
			name:              "peer not found",
			storageClusterUID: "non-existing-uid",
			existingObjs:      []client.Object{peer1},
			expectError:       true,
		},
		{
			name:              "empty storage cluster UID",
			storageClusterUID: "",
			existingObjs:      []client.Object{peer1},
			expectError:       true,
		},
		{
			name:              "no peers exist",
			storageClusterUID: "storage-cluster-uid-123",
			existingObjs:      []client.Object{},
			expectError:       true,
		},
		{
			name:              "peer in different namespace - should not be found",
			storageClusterUID: "storage-cluster-uid-123",
			existingObjs:      []client.Object{peerDifferentNS},
			expectError:       true,
		},
		{
			name:              "peer with nil peer info",
			storageClusterUID: "storage-cluster-uid-123",
			existingObjs:      []client.Object{peerWithNilInfo},
			expectError:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			scheme, err := newScheme()
			assert.NoError(t, err)

			fakeClient := fake.NewClientBuilder().WithScheme(scheme).WithObjects(tt.existingObjs...).Build()
			scpManager := createTestStorageClusterPeerManager(fakeClient)

			result, err := scpManager.GetByPeerStorageClusterUID(context.TODO(), types.UID(tt.storageClusterUID))

			if tt.expectError {
				assert.Error(t, err)
				assert.Nil(t, result)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, result)
				assert.Equal(t, tt.expectedName, result.Name)
				assert.Equal(t, tt.storageClusterUID, result.Status.PeerInfo.StorageClusterUid)

			}
		})
	}
}
