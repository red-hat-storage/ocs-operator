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
	// TODO: Needs design discussion. The old impl correlated blocklist IPs
	// with per-image watchers (via PV cache) to map to nodes. Without the
	// cache we don't have that mapping. Remote consumers also need rethinking.
}
