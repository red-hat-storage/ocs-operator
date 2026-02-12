package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

var _ prometheus.Collector = &CephFSSubvolumeCountCollector{}

type CephFSSubvolumeCountCollector struct {
	TotalSubvolumeCount *prometheus.Desc
}

func NewCephFSSubvolumeCountCollector() *CephFSSubvolumeCountCollector {
	return &CephFSSubvolumeCountCollector{
		TotalSubvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"Total number of CephFS Subvolumes", nil, nil),
	}
}

func (c *CephFSSubvolumeCountCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.TotalSubvolumeCount
}

func (c *CephFSSubvolumeCountCollector) Collect(ch chan<- prometheus.Metric) {
	// TODO: re-implement without cache, query ceph fs subvolume ls directly
}
