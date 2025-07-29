package collectors

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"k8s.io/apimachinery/pkg/fields"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &CephRBDChildrenCollector{}

type CephRBDChildrenCollector struct {
	Informer              cache.SharedIndexInformer
	kubeClient            clientset.Interface
	Namespace             string
	PersistentVolumeStore *internalcache.PersistentVolumeStore
	// Metrics Descriptors
	RBDChildrenCount *prometheus.Desc
}

func NewCephRBDChildrenCollector(pvStore *internalcache.PersistentVolumeStore, opts *options.Options) *CephRBDChildrenCollector {
	ocsClient, err := GetOcsV1alpha1Client(opts)
	if err != nil {
		klog.Error(err)
		return nil
	}
	ns := searchInNamespace(opts)
	lw := cache.NewListWatchFromClient(ocsClient, "storageconsumers", ns, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &ocsv1alpha1.StorageConsumer{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephRBDChildrenCollector{
		kubeClient:            clientset.NewForConfigOrDie(opts.Kubeconfig),
		PersistentVolumeStore: pvStore,
		RBDChildrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"RBD children count", []string{"image", "radosnamespace", "consumerid"}, nil),
		Informer:  sharedIndexInformer,
		Namespace: ns,
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

	consumerId := c.GetStorageConsumerId()
	if consumerId == "" {
		klog.Error("consumerId is empty")
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
			image, radosNamespace, consumerId,
		)
	}
}
func (c *CephRBDChildrenCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

func (c *CephRBDChildrenCollector) GetStorageConsumerId() string {
	indexer := c.Informer.GetIndexer()
	key := fmt.Sprintf("%s/%s", c.Namespace, defaults.LocalStorageConsumerName)
	obj, exists, err := indexer.GetByKey(key)
	if err != nil {
		klog.Errorf("Error getting StorageConsumer %q: %v", key, err)
		return ""
	}
	if !exists {
		klog.Warningf("StorageConsumer %q not found in cache", key)
		return ""
	}

	sc, ok := obj.(*ocsv1alpha1.StorageConsumer)
	if !ok {
		klog.Errorf("Object found with key %q is not a StorageConsumer", key)
		return ""
	}
	return string(sc.GetUID())
}
