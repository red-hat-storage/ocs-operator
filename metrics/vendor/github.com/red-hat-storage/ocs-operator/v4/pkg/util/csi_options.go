package util

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

// GetCephFSKernelMountOptions returns the kernel mount options for CephFS based on the spec on the StorageCluster
func GetCephFSKernelMountOptions(sc *ocsv1.StorageCluster) string {
	// If Encryption is enabled, Always use secure mode
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil &&
		sc.Spec.Network.Connections.Encryption != nil && sc.Spec.Network.Connections.Encryption.Enabled {
		return "ms_mode=secure"
	}

	// If encryption is not enabled, use prefer-crc mode
	return "ms_mode=prefer-crc"
}

// getReadAffinityyOptions returns the read affinity options based on the spec on the StorageCluster.
func GetReadAffinityOptions(sc *ocsv1.StorageCluster) rookCephv1.ReadAffinitySpec {
	if sc.Spec.CSI != nil && sc.Spec.CSI.ReadAffinity != nil {
		return *sc.Spec.CSI.ReadAffinity
	}

	return rookCephv1.ReadAffinitySpec{
		Enabled: !sc.Spec.ExternalStorage.Enable,
	}
}
