package util

import (
	"fmt"
	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NetworkFenceClassType string

const (
	RbdNetworkFenceClass NetworkFenceClassType = "rbd"
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
