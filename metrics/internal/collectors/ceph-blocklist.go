package collectors

import (
	"regexp"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"golang.org/x/net/context"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &CephBlocklistCollector{}

type CephBlocklistCollector struct {
	kubeClient            clientset.Interface
	CephBlocklistStore    *internalcache.CephBlocklistStore
	PersistentVolumeStore *internalcache.PersistentVolumeStore
	// Metrics Descriptors
	NodeRBDBlocklist *prometheus.Desc
}

func NewCephBlocklistCollector(blocklistStore *internalcache.CephBlocklistStore, pvStore *internalcache.PersistentVolumeStore, opts *options.Options) *CephBlocklistCollector {
	return &CephBlocklistCollector{
		kubeClient:            clientset.NewForConfigOrDie(opts.Kubeconfig),
		CephBlocklistStore:    blocklistStore,
		PersistentVolumeStore: pvStore,
		NodeRBDBlocklist: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "rbd_client_blocklisted"),
			"State of the rbd client on a node, 0 = Unblocked, 1 = Blocked", []string{"node"}, nil),
	}
}

func (c *CephBlocklistCollector) getNodes() (map[string]bool, error) {
	nodes, err := c.kubeClient.CoreV1().Nodes().List(context.Background(), metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	// Initialize the nodesBlocked map with all nodes set to false
	nodesBlocked := make(map[string]bool)
	for _, node := range nodes.Items {
		nodesBlocked[node.Name] = false
	}
	return nodesBlocked, nil
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

	blockedNodes, err := c.getNodes()
	if err != nil {
		klog.Errorf("failed to get nodes: %v", err)
	}

	for client, nodes := range c.PersistentVolumeStore.RBDClientMap {
		ip, port, nonce := parseAddress(client)
		if ip == "" {
			klog.Errorf("error parsing address %s", client)
			continue
		}
		for _, node := range nodes {
			if c.CephBlocklistStore.IsBlocked(ip, port, nonce) {
				blockedNodes[node] = true
			}
		}
	}

	for node, blocked := range blockedNodes {
		if blocked {
			ch <- prometheus.MustNewConstMetric(c.NodeRBDBlocklist, prometheus.GaugeValue, 1, node)
		} else {
			ch <- prometheus.MustNewConstMetric(c.NodeRBDBlocklist, prometheus.GaugeValue, 0, node)
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
