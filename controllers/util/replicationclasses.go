package util

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"

	replicationv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/replication.storage/v1alpha1"
	templatev1 "github.com/openshift/api/template/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func VolumeReplicationClassFromTemplate(
	ctx context.Context,
	kubeClient client.Client,
	volumeReplicationClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	remoteRbdStorageId string,
) (*replicationv1alpha1.VolumeReplicationClass, error) {
	replicationClassName := volumeReplicationClassName
	//TODO: The code is written under the assumption VRC name is exactly the same as the template name and there
	// is 1:1 mapping between template and vrc. The restriction will be relaxed in the future
	vrcTemplate := &templatev1.Template{}
	vrcTemplate.Name = replicationClassName
	vrcTemplate.Namespace = consumer.Namespace

	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(vrcTemplate), vrcTemplate); err != nil {
		return nil, fmt.Errorf("failed to get VolumeReplicationClass template: %s, %v", replicationClassName, err)
	}

	if len(vrcTemplate.Objects) != 1 {
		return nil, fmt.Errorf("unexpected number of Volume Replication Class found expected 1")
	}

	vrc := &replicationv1alpha1.VolumeReplicationClass{}
	if err := json.Unmarshal(vrcTemplate.Objects[0].Raw, vrc); err != nil {
		return nil, fmt.Errorf("failed to unmarshall volume replication class: %s, %v", replicationClassName, err)

	}

	if vrc.Name != replicationClassName {
		return nil, fmt.Errorf("volume replication class name mismatch: %s, %v", replicationClassName, vrc.Name)
	}

	switch vrc.Spec.Provisioner {
	case RbdDriverName:
		var replicationID string
		if strings.Compare(rbdStorageId, remoteRbdStorageId) <= 0 {
			replicationID = CalculateMD5Hash([]string{rbdStorageId, remoteRbdStorageId})
		} else {
			replicationID = CalculateMD5Hash([]string{remoteRbdStorageId, rbdStorageId})
		}
		vrc.Spec.Parameters["replication.storage.openshift.io/replication-secret-name"] = consumerConfig.GetCsiRbdProvisionerCephUserName()
		vrc.Spec.Parameters["replication.storage.openshift.io/replication-secret-namespace"] = consumer.Status.Client.OperatorNamespace
		vrc.Spec.Parameters["clusterID"] = consumerConfig.GetRbdClientProfileName()
		AddLabel(vrc, ramenDRStorageIDLabelKey, rbdStorageId)
		AddLabel(vrc, ramenDRReplicationIDLabelKey, replicationID)
	default:
		return nil, UnsupportedProvisioner
	}
	return vrc, nil
}
