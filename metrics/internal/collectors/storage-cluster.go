package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
)

type StorageClusterCollector struct {
	KMSServerConnectionStatus *prometheus.Desc
	Informer                  cache.SharedIndexInformer
	AllowedNamespace          string
}

var _ prometheus.Collector = &StorageClusterCollector{}

func initClient(opts *options.Options) (*rest.RESTClient, error) {
	ocsConfig, err := clientcmd.BuildConfigFromFlags("", opts.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(ocsv1alpha1.AddToScheme(scheme))
	codecs := serializer.NewCodecFactory(scheme)
	ocsConfig.GroupVersion = &ocsv1alpha1.GroupVersion
	ocsConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	ocsConfig.APIPath = "/apis"
	ocsConfig.ContentType = runtime.ContentTypeJSON
	if ocsConfig.UserAgent == "" {
		ocsConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	ocsClient, err := rest.RESTClientFor(ocsConfig)
	if err != nil {
		return nil, err
	}
	return ocsClient, nil
}

type StorageClusterLister interface {
	List(selector labels.Selector) (storageclusters []*v1.StorageCluster, err error)
}

type storageClusterLister struct {
	indexer cache.Indexer
}

func NewStorageClusterLister(indexer cache.Indexer) StorageClusterLister {
	return &storageClusterLister{indexer: indexer}
}

func (s *storageClusterLister) List(selector labels.Selector) (storageclusters []*v1.StorageCluster, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		storageclusters = append(storageclusters, m.(*v1.StorageCluster))
	})
	return storageclusters, err
}

func NewStorageClusterCollector(opts *options.Options) *StorageClusterCollector {
	cl, err := initClient(opts)
	if err != nil {
		klog.Errorf("Unable to get initialize client: %v", err)
		return nil
	}
	lw := cache.NewListWatchFromClient(cl, "storageclusters", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &v1.StorageCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return &StorageClusterCollector{
		KMSServerConnectionStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "storagecluster", "kms_connection_status"),
			`KMS Connection Status; 0: Connected, 1: Failure.`,
			[]string{"name", "namespace", "kms_connection_error", "kms_server_address"},
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
	storageClusterrLister := NewStorageClusterLister(c.Informer.GetIndexer())
	storageClusters := getAllStorageClusters(storageClusterrLister, c.AllowedNamespace)
	if len(storageClusters) > 0 {
		c.collectKMSConnectionStatuses(ch, storageClusters)
	}
}

func getAllStorageClusters(lister StorageClusterLister, namespace string) (storageClusters []*v1.StorageCluster) {
	storageClusters, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageCluster: %v", err)
		return nil
	}
	return
}

func (c *StorageClusterCollector) collectKMSConnectionStatuses(ch chan<- prometheus.Metric, storageClusters []*v1.StorageCluster) {
	for _, storageCluster := range storageClusters {
		v := 0
		if storageCluster.Status.KMSServerConnection.KMSServerConnectionError != "" {
			v = 1
		}
		ch <- prometheus.MustNewConstMetric(
			c.KMSServerConnectionStatus,
			prometheus.GaugeValue,
			float64(v),
			storageCluster.Name,
			storageCluster.Namespace,
			storageCluster.Status.KMSServerConnection.KMSServerConnectionError,
			storageCluster.Status.KMSServerConnection.KMSServerAddress,
		)
	}
}
