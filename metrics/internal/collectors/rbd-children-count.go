package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

var _ prometheus.Collector = &CephRBDChildrenCollector{}

type CephRBDChildrenCollector struct {
	RBDChildrenCount *prometheus.Desc
}

func NewCephRBDChildrenCollector() *CephRBDChildrenCollector {
	return &CephRBDChildrenCollector{
		RBDChildrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"RBD children count", []string{"image", "radosnamespace", "consumerid"}, nil),
	}
}

func (c *CephRBDChildrenCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.RBDChildrenCount
}

func (c *CephRBDChildrenCollector) Collect(ch chan<- prometheus.Metric) {
	// TODO: re-implement without cache, query rbd children counts and storage consumer directly
}
