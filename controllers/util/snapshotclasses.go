package util

import (
	"context"
	"errors"
	"fmt"

	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// SnapshotterType represents a snapshotter type
type SnapshotterType string

const (
	RbdSnapshotter    SnapshotterType = "rbd"
	CephfsSnapshotter SnapshotterType = "cephfs"
	NfsSnapshotter    SnapshotterType = "nfs"
)

var (
	UnsupportedDriver = errors.New("unsupportedDriver")
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
	storageId string,
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
	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultCephFsSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	storageId string,
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
	if storageId != "" {
		AddLabel(sc, storageIdLabelKey, storageId)
	}
	return sc
}

func NewDefaultNfsSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace string,
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
	return sc
}

func VolumeSnapshotClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	volumeSnapshotClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	cephFsStorageId,
	nfsStorageId string,
) (*snapapi.VolumeSnapshotClass, error) {
	snapshotClass := &snapapi.VolumeSnapshotClass{}
	snapshotClass.Name = volumeSnapshotClassName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(snapshotClass), snapshotClass); err != nil {
		return nil, err
	}
	clientProfileName := ""
	provisionerSecretName := ""
	storageId := ""
	operatorNamespace := consumer.Status.Client.OperatorNamespace
	switch snapshotClass.Driver {
	case RbdDriverName:
		clientProfileName = consumerConfig.GetRbdClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiRbdProvisionerSecretName()
		storageId = rbdStorageId
	case CephFSDriverName:
		clientProfileName = consumerConfig.GetCephFsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiCephFsProvisionerSecretName()
		storageId = cephFsStorageId
	case NfsDriverName:
		clientProfileName = consumerConfig.GetNfsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiNfsProvisionerSecretName()
		storageId = nfsStorageId
	default:
		return nil, UnsupportedDriver
	}

	params := snapshotClass.Parameters
	if params == nil {
		params = map[string]string{}
		snapshotClass.Parameters = params
	}
	params["clusterID"] = clientProfileName
	params["csi.storage.k8s.io/snapshotter-secret-name"] = provisionerSecretName
	params["csi.storage.k8s.io/snapshotter-secret-namespace"] = operatorNamespace
	AddLabel(snapshotClass, storageIdLabelKey, storageId)
	return snapshotClass, nil
}
