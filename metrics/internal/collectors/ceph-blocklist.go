package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
)

var _ prometheus.Collector = &CephBlocklistCollector{}

type CephBlocklistCollector struct {
	NodeRBDBlocklist *prometheus.Desc
}

func NewCephBlocklistCollector() *CephBlocklistCollector {
	return &CephBlocklistCollector{
		NodeRBDBlocklist: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "rbd_client_blocklisted"),
			"State of the rbd client on a node, 0 = Unblocked, 1 = Blocked", []string{"node"}, nil),
	}
}

func (c *CephBlocklistCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.NodeRBDBlocklist
}

func (c *CephBlocklistCollector) Collect(ch chan<- prometheus.Metric) {
	// TODO: re-implement without cache, query ceph osd blocklist and correlate with nodes directly
}
