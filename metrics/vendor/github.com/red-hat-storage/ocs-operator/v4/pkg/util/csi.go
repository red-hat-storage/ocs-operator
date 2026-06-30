package util

const (
	StorageClassDriverNamePrefix = "openshift-storage"
	RbdDriverNameSuffix          = ".rbd.csi.ceph.com"
	CephFSDriverNameSuffix       = ".cephfs.csi.ceph.com"
	NfsDriverNameSuffix          = ".nfs.csi.ceph.com"
	ObcDriverNameSuffix          = ".ceph.rook.io/bucket"
	RbdDriverName                = StorageClassDriverNamePrefix + RbdDriverNameSuffix
	CephFSDriverName             = StorageClassDriverNamePrefix + CephFSDriverNameSuffix
	NfsDriverName                = StorageClassDriverNamePrefix + NfsDriverNameSuffix
	ObcDriverName                = StorageClassDriverNamePrefix + ObcDriverNameSuffix
)

var (
	SupportedCsiDrivers = []string{
		RbdDriverName,
		CephFSDriverName,
		NfsDriverName,
	}
)
