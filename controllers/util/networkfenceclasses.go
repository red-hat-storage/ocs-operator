package util

import (
	"fmt"
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
