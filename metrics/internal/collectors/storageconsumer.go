package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &StorageConsumerCollector{}

type StorageConsumerCollector struct {
	Informer                cache.SharedIndexInformer
	StorageConsumerMetadata *prometheus.Desc
	AllowedNamespace        string
}

func NewStorageConsumerCollector(opts *options.Options) *StorageConsumerCollector {
	ocsClient, err := GetOcsV1alpha1Client(opts)
	if err != nil {
		klog.Error(err)
		return nil
	}
	lw := cache.NewListWatchFromClient(ocsClient, "storageconsumers", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &ocsv1alpha1.StorageConsumer{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return &StorageConsumerCollector{
		StorageConsumerMetadata: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_consumer", "metadata"),
			`Attributes of OCS Storage Consumers`,
			[]string{"storage_consumer_name", "state"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

func (c *StorageConsumerCollector) Collect(ch chan<- prometheus.Metric) {
	storageConsumerLister := NewStorageConsumerLister(c.Informer.GetIndexer())
	storageConsumers := getAllStorageConsumers(storageConsumerLister, c.AllowedNamespace)
	if len(storageConsumers) > 0 {
		c.collectStorageConsumersMetadata(storageConsumers, ch)
	}
}

func (c *StorageConsumerCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.StorageConsumerMetadata,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (c *StorageConsumerCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

func (c *StorageConsumerCollector) collectStorageConsumersMetadata(storageConsumers []*ocsv1alpha1.StorageConsumer, ch chan<- prometheus.Metric) {
	for _, storageConsumer := range storageConsumers {
		ch <- prometheus.MustNewConstMetric(c.StorageConsumerMetadata,
			prometheus.GaugeValue, 1,
			storageConsumer.Name,
			string(storageConsumer.Status.State))
	}
}

func getAllStorageConsumers(lister StorageConsumerLister, namespace string) (storageConsumers []*ocsv1alpha1.StorageConsumer) {
	storageConsumers, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageConsumer. %v", err)
		return nil
	}
	return
}
