package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
)

type StorageClusterCollector struct {
	KMSServerConnectionStatus *prometheus.Desc
	Informer                  cache.SharedIndexInformer
}

var _ prometheus.Collector = &StorageClusterCollector{}

func NewStorageClusterCollector(opts *options.Options) *StorageClusterCollector {
	cl, err := GetOcsV1Client(opts)
	if err != nil {
		klog.Errorf("Unable to get client: %v", err)
		return nil
	}
	lw := cache.NewListWatchFromClient(cl, "storageclusters", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &v1.StorageCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return &StorageClusterCollector{
		KMSServerConnectionStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "storagecluster", "kms_connection_status"),
			`KMS Connection Status; 0: Connected, 1: Not Connected, 2: KMS not enabled`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

func (c *StorageClusterCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

func (c *StorageClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.KMSServerConnectionStatus,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (c *StorageClusterCollector) Collect(ch chan<- prometheus.Metric) {
	scLister := NewStorageClusterLister(c.Informer.GetIndexer())
	storageClusters := getAllStorageClusters(scLister)
	if len(storageClusters) > 0 {
		c.collectKMSConnectionStatuses(ch, storageClusters)
	}
}

func getAllStorageClusters(lister StorageClusterLister) []*v1.StorageCluster {
	storageClusters, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageCluster: %v", err)
		return nil
	}
	return storageClusters
}

func (c *StorageClusterCollector) collectKMSConnectionStatuses(ch chan<- prometheus.Metric, storageClusters []*v1.StorageCluster) {
	for _, storageCluster := range storageClusters {
		v := 2
		if storageCluster.Spec.Encryption.Enable || storageCluster.Spec.Encryption.ClusterWide {
			v = 0
			if storageCluster.Status.KMSServerConnection.KMSServerConnectionError != "" {
				v = 1
			}
		}
		ch <- prometheus.MustNewConstMetric(
			c.KMSServerConnectionStatus,
			prometheus.GaugeValue,
			float64(v),
			storageCluster.Name,
			storageCluster.Namespace,
		)
	}
}
