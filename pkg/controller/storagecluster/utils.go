package storagecluster

import (
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
)

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(devicesets []ocsv1.StorageDeviceSet) []rook.StorageClassDeviceSet {
	var scds []rook.StorageClassDeviceSet

	for _, ds := range devicesets {
		scds = append(scds, ds.ToStorageClassDeviceSet())
	}

	return scds
}
