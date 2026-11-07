package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &StorageClusterPeerCollector{}

type StorageClusterPeerCollector struct {
	Informer   cache.SharedIndexInformer
	CertStatus *prometheus.Desc
}

func NewStorageClusterPeerCollector(opts *options.Options) *StorageClusterPeerCollector {
	ocsClient, err := GetOcsV1Client(opts)
	if err != nil {
		klog.Error(err)
		return nil
	}
	lw := cache.NewListWatchFromClient(ocsClient, "storageclusterpeers", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &ocsv1.StorageClusterPeer{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &StorageClusterPeerCollector{
		CertStatus: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_cluster_peer", "cert_status"),
			`mTLS certificate configuration status of OCS StorageClusterPeers (0=configured, 1=not configured in spec)`,
			[]string{"storage_cluster_peer_name", "namespace", "cert_type"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

func (c *StorageClusterPeerCollector) Collect(ch chan<- prometheus.Metric) {
	lister := NewStorageClusterPeerLister(c.Informer.GetIndexer())
	peers, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageClusterPeer. %v", err)
		return
	}
	for _, peer := range peers {
		ch <- prometheus.MustNewConstMetric(c.CertStatus,
			prometheus.GaugeValue, boolToFloat64(peer.Spec.ServerCAConfigMap.Name == ""),
			peer.Name, peer.Namespace, "server_ca")

		ch <- prometheus.MustNewConstMetric(c.CertStatus,
			prometheus.GaugeValue, boolToFloat64(peer.Spec.ClientCertSecret.Name == ""),
			peer.Name, peer.Namespace, "client_cert")

		ch <- prometheus.MustNewConstMetric(c.CertStatus,
			prometheus.GaugeValue, boolToFloat64(peer.Spec.ClientCAConfigMap.Name == ""),
			peer.Name, peer.Namespace, "client_ca")
	}
}

func (c *StorageClusterPeerCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.CertStatus
}

func (c *StorageClusterPeerCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

type StorageClusterPeerLister interface {
	List(selector labels.Selector) (ret []*ocsv1.StorageClusterPeer, err error)
}

type storageClusterPeerLister struct {
	indexer cache.Indexer
}

func NewStorageClusterPeerLister(indexer cache.Indexer) StorageClusterPeerLister {
	return &storageClusterPeerLister{indexer: indexer}
}

func (s *storageClusterPeerLister) List(selector labels.Selector) (ret []*ocsv1.StorageClusterPeer, err error) {
	err = cache.ListAll(s.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*ocsv1.StorageClusterPeer))
	})
	return ret, err
}
