package ceph

import (
	"fmt"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
)

func BuildMirrorPeerMap(ioctx *rados.IOContext) (map[string]string, error) {
	peers, err := rbd.ListMirrorPeerSite(ioctx)
	if err != nil {
		return nil, fmt.Errorf("failed to list mirror peer sites: %w", err)
	}

	peerMap := make(map[string]string, len(peers))
	for _, peer := range peers {
		peerMap[peer.MirrorUUID] = peer.SiteName
	}
	return peerMap, nil
}
