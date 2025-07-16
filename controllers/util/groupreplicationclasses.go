package util

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	templatev1 "github.com/openshift/api/template/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ramenDRStorageIDLabelKey     = "ramendr.openshift.io/storageid"
	ramenDRReplicationIDLabelKey = "ramendr.openshift.io/replicationid"
	ramenMaintenanceModeLabelKey = "ramendr.openshift.io/maintenancemodes"
)

func VolumeGroupReplicationClassFromTemplate(
	ctx context.Context,
	kubeClient client.Client,
	volumeGroupReplicationClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	remoteRbdStorageId string,
) (*replicationv1alpha1.VolumeGroupReplicationClass, error) {
	replicationClassName := volumeGroupReplicationClassName
	//TODO: The code is written under the assumption VGRC name is exactly the same as the template name and there
	// is 1:1 mapping between template and vgrc. The restriction will be relaxed in the future
	vgrcTemplate := &templatev1.Template{}
	vgrcTemplate.Name = replicationClassName
	vgrcTemplate.Namespace = consumer.Namespace

	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(vgrcTemplate), vgrcTemplate); err != nil {
		return nil, fmt.Errorf("failed to get VolumeGroupReplicationClass template: %s, %v", replicationClassName, err)
	}

	if len(vgrcTemplate.Objects) != 1 {
		return nil, fmt.Errorf("unexpected number of Volume Group Replication Class found expected 1")
	}

	vgrc := &replicationv1alpha1.VolumeGroupReplicationClass{}
	if err := json.Unmarshal(vgrcTemplate.Objects[0].Raw, vgrc); err != nil {
		return nil, fmt.Errorf("failed to unmarshall volume group replication class: %s, %v", replicationClassName, err)

	}

	if vgrc.Name != replicationClassName {
		return nil, fmt.Errorf("volume group replication class name mismatch: %s, %v", replicationClassName, vgrc.Name)
	}

	switch vgrc.Spec.Provisioner {
	case RbdDriverName:
		// For VGRC the replicationID will be a combination of RBDStorageID, RemoteRBDStorageID and the poolName
		// pool name is added to the VGRC's template
		poolName := vgrc.Spec.Parameters["pool"]
		storageIDs := []string{rbdStorageId, remoteRbdStorageId, poolName}
		slices.Sort(storageIDs)
		replicationID := CalculateMD5Hash(storageIDs)
		vgrc.Spec.Parameters["replication.storage.openshift.io/group-replication-secret-name"] = consumerConfig.GetCsiRbdProvisionerCephUserName()
		vgrc.Spec.Parameters["replication.storage.openshift.io/group-replication-secret-namespace"] = consumer.Status.Client.OperatorNamespace
		vgrc.Spec.Parameters["clusterID"] = consumerConfig.GetRbdClientProfileName()
		AddLabel(vgrc, ramenDRStorageIDLabelKey, rbdStorageId)
		AddLabel(vgrc, ramenMaintenanceModeLabelKey, "Failover")
		AddLabel(vgrc, ramenDRReplicationIDLabelKey, replicationID)
	default:
		return nil, fmt.Errorf("unsupported Provisioner for VolumeGroupReplicationClass")
	}
	return vgrc, nil
}
