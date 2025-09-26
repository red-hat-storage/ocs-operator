package util

import (
	"context"
	"fmt"
	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type NetworkFenceClassType string

const (
	RbdNetworkFenceClass    NetworkFenceClassType = "rbd"
	CephfsNetworkFenceClass NetworkFenceClassType = "cephfs"
)

func GenerateNameForNetworkFenceClass(storageClusterName string, networkFenceClassType NetworkFenceClassType) string {
	if networkFenceClassType == RbdNetworkFenceClass {
		return fmt.Sprintf("%s-ceph-%s-networkfenceclass", storageClusterName, networkFenceClassType)
	}
	return fmt.Sprintf("%s-%s-networkfenceclass", storageClusterName, networkFenceClassType)
}

func NewDefaultRbdNetworkFenceClass(
	provisionerSecret,
	namespace,
	storageId string,
) *csiaddonsv1alpha1.NetworkFenceClass {

	nfc := &csiaddonsv1alpha1.NetworkFenceClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: csiaddonsv1alpha1.NetworkFenceClassSpec{
			Provisioner: RbdDriverName,
			Parameters: map[string]string{
				"csiaddons.openshift.io/networkfence-secret-name":      provisionerSecret,
				"csiaddons.openshift.io/networkfence-secret-namespace": namespace,
			},
		},
	}
	if storageId != "" {
		AddAnnotation(nfc, storageIdLabelKey, storageId)
	}
	return nfc
}

func NewDefaultCephFsNetworkFenceClass(
	provisionerSecret,
	namespace,
	storageId string,
) *csiaddonsv1alpha1.NetworkFenceClass {

	nfc := &csiaddonsv1alpha1.NetworkFenceClass{
		ObjectMeta: metav1.ObjectMeta{
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: csiaddonsv1alpha1.NetworkFenceClassSpec{
			Provisioner: CephFSDriverName,
			Parameters: map[string]string{
				"csiaddons.openshift.io/networkfence-secret-name":      provisionerSecret,
				"csiaddons.openshift.io/networkfence-secret-namespace": namespace,
			},
		},
	}
	if storageId != "" {
		AddAnnotation(nfc, storageIdLabelKey, storageId)
	}
	return nfc
}

func NetworkFenceClassFromExisting(
	ctx context.Context,
	kubeClient client.Client,
	networkFenceClassName string,
	consumer *ocsv1a1.StorageConsumer,
	consumerConfig StorageConsumerResources,
	rbdStorageId,
	cephFsStorageId string,
) (*csiaddonsv1alpha1.NetworkFenceClass, error) {
	nfc := &csiaddonsv1alpha1.NetworkFenceClass{}
	nfc.Name = networkFenceClassName
	if err := kubeClient.Get(ctx, client.ObjectKeyFromObject(nfc), nfc); err != nil {
		return nil, err
	}
	provisionerSecretName := ""
	storageId := ""
	operatorNamespace := consumer.Status.Client.OperatorNamespace
	switch nfc.Spec.Provisioner {
	case RbdDriverName:
		provisionerSecretName = consumerConfig.GetCsiRbdProvisionerCephUserName()
		storageId = rbdStorageId
	case CephFSDriverName:
		provisionerSecretName = consumerConfig.GetCsiCephFsProvisionerCephUserName()
		storageId = cephFsStorageId
	default:
		return nil, UnsupportedProvisioner
	}

	params := nfc.Spec.Parameters
	if params == nil {
		params = map[string]string{}
		nfc.Spec.Parameters = params
	}
	params["csiaddons.openshift.io/networkfence-secret-name"] = provisionerSecretName
	params["csiaddons.openshift.io/networkfence-secret-namespace"] = operatorNamespace
	AddAnnotation(nfc, storageIdLabelKey, storageId)
	return nfc, nil
}
