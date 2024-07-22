package util

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

// GetCephFSKernelMountOptions returns the kernel mount options for CephFS based on the spec on the StorageCluster
func GetCephFSKernelMountOptions(sc *ocsv1.StorageCluster) string {
	// If Encryption is enabled, Always use secure mode
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil &&
		sc.Spec.Network.Connections.Encryption != nil && sc.Spec.Network.Connections.Encryption.Enabled {
		return "ms_mode=secure"
	}

	// If Encryption is not enabled, but Compression or RequireMsgr2 is enabled, use prefer-crc mode
	if sc.Spec.Network != nil && sc.Spec.Network.Connections != nil &&
		((sc.Spec.Network.Connections.Compression != nil && sc.Spec.Network.Connections.Compression.Enabled) ||
			sc.Spec.Network.Connections.RequireMsgr2) {
		return "ms_mode=prefer-crc"
	}

	// Network spec always has higher precedence even in the External or Provider cluster. so they are checked first above

	// None of Encryption, Compression, RequireMsgr2 are enabled on the StorageCluster
	// If it's an External or Provider cluster, We don't require msgr2 by default so no mount options are needed
	if sc.Spec.ExternalStorage.Enable || sc.Spec.AllowRemoteStorageConsumers {
		return "ms_mode=legacy"
	}
	// If none of the above cases apply, We set RequireMsgr2 true by default on the cephcluster
	// so we need to set the mount options to prefer-crc
	return "ms_mode=prefer-crc"
}
