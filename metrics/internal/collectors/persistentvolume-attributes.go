package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
)

var _ prometheus.Collector = &PersistentVolumeAttributesCollector{}

type PersistentVolumeAttributesCollector struct {
	Store      *internalcache.PersistentVolumeStore
	PVMetadata *prometheus.Desc
}

func NewPersistentVolumeAttributesCollector(store *internalcache.PersistentVolumeStore, opts *options.Options) *PersistentVolumeAttributesCollector {
	var _ = opts
	return &PersistentVolumeAttributesCollector{
		Store: store,
		PVMetadata: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "rbd", "pv_metadata"),
			`Attributes of Ceph RBD based Persistent Volume`,
			[]string{"name", "image", "pool_name"},
			nil,
		),
	}
}

// Describe implements prometheus.Collector interface
func (c *PersistentVolumeAttributesCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.PVMetadata,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *PersistentVolumeAttributesCollector) Collect(ch chan<- prometheus.Metric) {
	c.Store.Mutex.RLock()
	defer c.Store.Mutex.RUnlock()

	for _, attr := range c.Store.Store {
		ch <- prometheus.MustNewConstMetric(c.PVMetadata,
			prometheus.GaugeValue, 1,
			attr.PersistentVolumeName, attr.ImageName, attr.Pool,
		)
	}
}
