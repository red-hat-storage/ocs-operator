package storagecluster

import (
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
)

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(devicesets []ocsv1.StorageDeviceSet) []rook.StorageClassDeviceSet {
	var scds []rook.StorageClassDeviceSet

	for _, ds := range devicesets {
		var s rook.StorageClassDeviceSet
		s = ds.ToStorageClassDeviceSet()
		//
		// Disable OSD portability, as it is creating problems with the
		// default and currently only setting of failureDomain=host.
		//
		// TODO: make it dynamic when we take advantage of zones
		//
		s.Portable = false

		scds = append(scds, s)
	}

	return scds
}
