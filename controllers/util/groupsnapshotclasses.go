package util

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type GroupSnapshotterType string

const (
	RbdGroupSnapshotter    GroupSnapshotterType = "rbd"
	CephfsGroupSnapshotter GroupSnapshotterType = "cephfs"
)

func GenerateNameForGroupSnapshotClass(initData *ocsv1.StorageCluster, groupSnapshotType GroupSnapshotterType) string {
	if groupSnapshotType == RbdGroupSnapshotter {
		return fmt.Sprintf("%s-ceph-%s-groupsnapclass", initData.Name, groupSnapshotType)
	}
	return fmt.Sprintf("%s-%s-groupsnapclass", initData.Name, groupSnapshotType)
}

func NewDefaultRbdGroupSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	pool,
	storageId string,
) *groupsnapapi.VolumeGroupSnapshotClass {

	gsc := &groupsnapapi.VolumeGroupSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: RbdDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/group-snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/group-snapshotter-secret-namespace": namespace,
			"pool": pool,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if storageId != "" {
		AddLabel(gsc, storageIdLabelKey, storageId)
	}
	return gsc
}

func NewDefaultCephFsGroupSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	fsName,
	storageId string,
) *groupsnapapi.VolumeGroupSnapshotClass {

	gsc := &groupsnapapi.VolumeGroupSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: CephFSDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/group-snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/group-snapshotter-secret-namespace": namespace,
			"fsName": fsName,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if storageId != "" {
		AddLabel(gsc, storageIdLabelKey, storageId)
	}
	return gsc
}

func NewDefaultOdfCephFsGroupSnapshotClass(
	clusterID,
	provisionerSecret,
	namespace,
	fsName,
	storageId string,
) *odfgsapiv1b1.VolumeGroupSnapshotClass {

	gsc := &odfgsapiv1b1.VolumeGroupSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Driver: CephFSDriverName,
		Parameters: map[string]string{
			"clusterID": clusterID,
			"csi.storage.k8s.io/group-snapshotter-secret-name":      provisionerSecret,
			"csi.storage.k8s.io/group-snapshotter-secret-namespace": namespace,
			"fsName": fsName,
		},
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
	}
	if storageId != "" {
		AddLabel(gsc, storageIdLabelKey, storageId)
	}
	return gsc
}

func VolumeGroupSnapshotClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	volumeGroupSnapshotClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	cephFsStorageId,
	nfsStorageId string,
) (*groupsnapapi.VolumeGroupSnapshotClass, error) {
	gsc := &groupsnapapi.VolumeGroupSnapshotClass{}
	gsc.Name = volumeGroupSnapshotClassName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(gsc), gsc); err != nil {
		return nil, err
	}
	clientProfileName := ""
	provisionerSecretName := ""
	storageId := ""
	operatorNamespace := consumer.Status.Client.OperatorNamespace
	switch gsc.Driver {
	case RbdDriverName:
		clientProfileName = consumerConfig.GetRbdClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiRbdProvisionerCephUserName()
		storageId = rbdStorageId
	case CephFSDriverName:
		clientProfileName = consumerConfig.GetCephFsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiCephFsProvisionerCephUserName()
		storageId = cephFsStorageId
	case NfsDriverName:
		clientProfileName = consumerConfig.GetNfsClientProfileName()
		provisionerSecretName = consumerConfig.GetCsiNfsProvisionerCephUserName()
		storageId = nfsStorageId
	default:
		return nil, UnsupportedDriver
	}

	params := gsc.Parameters
	if params == nil {
		params = map[string]string{}
		gsc.Parameters = params
	}
	params["clusterID"] = clientProfileName
	params["csi.storage.k8s.io/group-snapshotter-secret-name"] = provisionerSecretName
	params["csi.storage.k8s.io/group-snapshotter-secret-namespace"] = operatorNamespace
	AddLabel(gsc, storageIdLabelKey, storageId)
	return gsc, nil
}

func OdfVolumeGroupSnapshotClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	volumeGroupSnapshotClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	cephFsStorageId string,
) (*odfgsapiv1b1.VolumeGroupSnapshotClass, error) {
	gsc := &odfgsapiv1b1.VolumeGroupSnapshotClass{}
	gsc.Name = volumeGroupSnapshotClassName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(gsc), gsc); err != nil {
		return nil, err
	}
	if gsc.Driver != CephFSDriverName {
		return nil, UnsupportedDriver
	}

	params := gsc.Parameters
	if params == nil {
		params = map[string]string{}
		gsc.Parameters = params
	}
	params["clusterID"] = consumerConfig.GetCephFsClientProfileName()
	params["csi.storage.k8s.io/group-snapshotter-secret-name"] = consumerConfig.GetCsiCephFsProvisionerCephUserName()
	params["csi.storage.k8s.io/group-snapshotter-secret-namespace"] = consumer.Status.Client.OperatorNamespace
	AddLabel(gsc, storageIdLabelKey, cephFsStorageId)
	return gsc, nil
}
