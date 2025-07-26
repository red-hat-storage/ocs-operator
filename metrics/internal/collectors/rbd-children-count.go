package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	clientset "k8s.io/client-go/kubernetes"
)

var _ prometheus.Collector = &CephRBDChildrenCollector{}

type CephRBDChildrenCollector struct {
	kubeClient               clientset.Interface
	PersistentVolumeStore    *internalcache.PersistentVolumeStore
	StorageConsumerCollector *StorageConsumerCollector
	// Metrics Descriptors
	RBDChildrenCount *prometheus.Desc
}

func NewCephRBDChildrenCollector(pvStore *internalcache.PersistentVolumeStore, opts *options.Options, storageConsumerCollector *StorageConsumerCollector) *CephRBDChildrenCollector {
	return &CephRBDChildrenCollector{
		kubeClient:               clientset.NewForConfigOrDie(opts.Kubeconfig),
		PersistentVolumeStore:    pvStore,
		StorageConsumerCollector: storageConsumerCollector,
		RBDChildrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"RBD children count", []string{"image", "radosnamespace", "clientid"}, nil),
	}
}

func (c *CephRBDChildrenCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.RBDChildrenCount,
	}

	for _, d := range ds {
		ch <- d
	}
}

func (c *CephRBDChildrenCollector) Collect(ch chan<- prometheus.Metric) {
	c.PersistentVolumeStore.Mutex.RLock()
	defer c.PersistentVolumeStore.Mutex.RUnlock()

	var clientId string
	if c.StorageConsumerCollector != nil {
		clientId = c.StorageConsumerCollector.GetClientId()
	}

	for _, attrs := range c.PersistentVolumeStore.Store {
		image := attrs.ImageName
		radosNamespace := attrs.RadosNameSpace

		count, ok := c.PersistentVolumeStore.RBDChildrenMap[image]
		if !ok {
			continue // skip if no children count
		}

		ch <- prometheus.MustNewConstMetric(
			c.RBDChildrenCount,
			prometheus.GaugeValue,
			float64(count),
			image, radosNamespace, clientId,
		)
	}
}
