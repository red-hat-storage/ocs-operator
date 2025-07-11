package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	clientset "k8s.io/client-go/kubernetes"
)

var _ prometheus.Collector = &CephRBDChildrenCollector{}

type CephRBDChildrenCollector struct {
	kubeClient            clientset.Interface
	PersistentVolumeStore *internalcache.PersistentVolumeStore
	// Metrics Descriptors
	RBDChildrenCount *prometheus.Desc
}

func NewCephRBDChildrenCollector(pvStore *internalcache.PersistentVolumeStore, opts *options.Options) *CephRBDChildrenCollector {
	return &CephRBDChildrenCollector{
		kubeClient:            clientset.NewForConfigOrDie(opts.Kubeconfig),
		PersistentVolumeStore: pvStore,
		RBDChildrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"RBD children count", []string{}, nil),
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

	for image, count := range c.PersistentVolumeStore.RBDChildrenMap {

		ch <- prometheus.MustNewConstMetric(c.RBDChildrenCount,
			prometheus.GaugeValue, float64(count), image)
	}
}
