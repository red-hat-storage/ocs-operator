package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
)

var _ prometheus.Collector = &CephFSSubvolumeCountCollector{}

type CephFSSubvolumeCountCollector struct {
	CephfsPVStore *internalcache.CephFSPVStore
	// Metrics Descriptors
	TotalPVCount        *prometheus.Desc
	TotalSubvolumeCount *prometheus.Desc
}

func NewCephFSSubvolumeCountCollector(pvStore *internalcache.CephFSPVStore, opts *options.Options) *CephFSSubvolumeCountCollector {
	return &CephFSSubvolumeCountCollector{
		CephfsPVStore: pvStore,
		TotalPVCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "pv_count"),
			"Total number of CephFS Persistent Volumes", nil, nil),
		TotalSubvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"Total number of CephFS Subvolumes", nil, nil),
	}
}

func (c *CephFSSubvolumeCountCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.TotalPVCount,
		c.TotalSubvolumeCount,
	}

	for _, d := range ds {
		ch <- d
	}
}

func (c *CephFSSubvolumeCountCollector) Collect(ch chan<- prometheus.Metric) {
	c.CephfsPVStore.Mutex.RLock()
	totalPVCount := len(c.CephfsPVStore.Store)
	c.CephfsPVStore.Mutex.RUnlock()

	ch <- prometheus.MustNewConstMetric(
		c.TotalPVCount,
		prometheus.GaugeValue,
		float64(totalPVCount),
	)

	totalSubvolumeCount := 0
	c.CephfsPVStore.Mutex.RLock()
	for _, count := range c.CephfsPVStore.CephFSSubvolumeCountMap {
		totalSubvolumeCount += count
	}
	c.CephfsPVStore.Mutex.RUnlock()

	ch <- prometheus.MustNewConstMetric(
		c.TotalSubvolumeCount,
		prometheus.GaugeValue,
		float64(totalSubvolumeCount),
	)
}
