package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

var _ prometheus.Collector = &PersistentVolumeAttributesCollector{}

type PersistentVolumeAttributesCollector struct {
	PVMetadata *prometheus.Desc
}

func NewPersistentVolumeAttributesCollector() *PersistentVolumeAttributesCollector {
	return &PersistentVolumeAttributesCollector{
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
	ch <- c.PVMetadata
}

// Collect implements prometheus.Collector interface
func (c *PersistentVolumeAttributesCollector) Collect(ch chan<- prometheus.Metric) {
	// TODO: re-implement without cache, fetch PV attributes directly
}
