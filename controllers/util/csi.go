package util

const (
	StorageClassDriverNamePrefix = "openshift-storage"
	RbdDriverName                = StorageClassDriverNamePrefix + ".rbd.csi.ceph.com"
	CephFSDriverName             = StorageClassDriverNamePrefix + ".cephfs.csi.ceph.com"
	NfsDriverName                = StorageClassDriverNamePrefix + ".nfs.csi.ceph.com"
	ObcDriverName                = StorageClassDriverNamePrefix + ".ceph.rook.io/bucket"
)
