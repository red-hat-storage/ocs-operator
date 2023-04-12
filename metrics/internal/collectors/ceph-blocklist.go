package collectors

import (
	"regexp"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/internal/cache"
	"k8s.io/klog"
)

var _ prometheus.Collector = &CephBlocklistCollector{}

type CephBlocklistCollector struct {
	CephBlocklistStore    *internalcache.CephBlocklistStore
	PersistentVolumeStore *internalcache.PersistentVolumeStore
	// Metrics Descriptors
	NodeRBDBlocklist *prometheus.Desc
}

func NewCephBlocklistCollector(blocklistStore *internalcache.CephBlocklistStore, pvStore *internalcache.PersistentVolumeStore) *CephBlocklistCollector {
	return &CephBlocklistCollector{
		CephBlocklistStore:    blocklistStore,
		PersistentVolumeStore: pvStore,
		NodeRBDBlocklist: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "rbd_client_blocklisted"),
			"State of the rbd client on a node, 0 = Unblocked, 1 = Blocked", []string{"node_name"}, nil),
	}
}

func (c *CephBlocklistCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.NodeRBDBlocklist,
	}

	for _, d := range ds {
		ch <- d
	}
}

func (c *CephBlocklistCollector) Collect(ch chan<- prometheus.Metric) {
	c.CephBlocklistStore.Mutex.RLock()
	defer c.CephBlocklistStore.Mutex.RUnlock()

	c.PersistentVolumeStore.Mutex.RLock()
	defer c.PersistentVolumeStore.Mutex.RUnlock()

	for node, clients := range c.PersistentVolumeStore.RBDClientMap {
		for _, watcher := range clients.Watchers {
			ip, port, nonce := parseAddress(watcher.Address)
			if ip == "" {
				klog.Errorf("error parsing address %s", watcher.Address)
				continue
			}

			if c.CephBlocklistStore.IsBlocked(ip, port, nonce) {
				ch <- prometheus.MustNewConstMetric(c.NodeRBDBlocklist, prometheus.GaugeValue, 1, node)
			} else {
				ch <- prometheus.MustNewConstMetric(c.NodeRBDBlocklist, prometheus.GaugeValue, 0, node)
			}
		}
	}
}

func parseAddress(address string) (string, int, int) {
	// Separate IP address, port and nonce from input string
	// IP address can contain square brackets if it's an IPv6 address
	re := regexp.MustCompile(`\[(.+)\]:(\d+)/(\d+)`)
	matches := re.FindStringSubmatch(address)
	if len(matches) == 4 {
		ip := matches[1]
		port, _ := strconv.Atoi(matches[2])
		nonce, _ := strconv.Atoi(matches[3])
		return ip, port, nonce
	}

	re = regexp.MustCompile(`(.+):(\d+)/(\d+)`)
	matches = re.FindStringSubmatch(address)
	if len(matches) == 4 {
		ip := matches[1]
		port, _ := strconv.Atoi(matches[2])
		nonce, _ := strconv.Atoi(matches[3])
		return ip, port, nonce
	}

	return "", 0, 0
}
