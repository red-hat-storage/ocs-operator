package util

import (
	"fmt"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// SnapshotterType represents a snapshotter type
type SnapshotterType string

const (
	RbdSnapshotter    SnapshotterType = "rbd"
	CephfsSnapshotter SnapshotterType = "cephfs"
	NfsSnapshotter    SnapshotterType = "nfs"
)

// GenerateNameForSnapshotClass function generates 'SnapshotClass' name.
// 'snapshotType' can be: 'RbdSnapshotter' or 'CephfsSnapshotter' or 'NfsSnapshotter'
func GenerateNameForSnapshotClass(storageClusterName string, snapshotType SnapshotterType) string {
	return fmt.Sprintf("%s-%splugin-snapclass", storageClusterName, snapshotType)
}

func NewDefaultRbdSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	drStorageID string,
) *snapapi.VolumeSnapshotClass {

	sc := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: RbdDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/snapshotter-secret-namespace": namespace,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	return sc
}

func NewDefaultCephFsSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	drStorageID string,
) *snapapi.VolumeSnapshotClass {

	sc := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: CephFSDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/snapshotter-secret-namespace": namespace,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	return sc
}

func NewDefaultNfsSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	drStorageID string,
) *snapapi.VolumeSnapshotClass {

	sc := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: NfsDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/snapshotter-secret-namespace": namespace,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if drStorageID != "" {
		AddLabel(sc, ramenDRStorageIDLabelKey, drStorageID)
	}
	return sc
}
