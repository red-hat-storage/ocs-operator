package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
)

// getReadAffinityyOptions returns the read affinity options based on the spec on the StorageCluster.
func getReadAffinityOptions(sc *ocsv1.StorageCluster) rookCephv1.ReadAffinitySpec {
	if sc.Spec.CSI != nil && sc.Spec.CSI.ReadAffinity != nil {
		return *sc.Spec.CSI.ReadAffinity
	}

	return rookCephv1.ReadAffinitySpec{
		Enabled: !sc.Spec.ExternalStorage.Enable,
	}
}
