package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
)

var _ prometheus.Collector = &CephFSSubvolumeCountCollector{}

type CephFSSubvolumeCountCollector struct {
	CephfsPVStore *internalcache.PersistentVolumeStore
	// Metrics Descriptors
	TotalSubvolumeCount *prometheus.Desc
}

func NewCephFSSubvolumeCountCollector(pvStore *internalcache.PersistentVolumeStore, opts *options.Options) *CephFSSubvolumeCountCollector {
	return &CephFSSubvolumeCountCollector{
		CephfsPVStore: pvStore,
		TotalSubvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"Total number of CephFS Subvolumes", nil, nil),
	}
}

func (c *CephFSSubvolumeCountCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.TotalSubvolumeCount,
	}

	for _, d := range ds {
		ch <- d
	}
}

func (c *CephFSSubvolumeCountCollector) Collect(ch chan<- prometheus.Metric) {
	c.CephfsPVStore.Mutex.RLock()
	defer c.CephfsPVStore.Mutex.RUnlock()

	ch <- prometheus.MustNewConstMetric(
		c.TotalSubvolumeCount,
		prometheus.GaugeValue,
		float64(c.CephfsPVStore.CephFSSubvolumeCount),
	)
}
