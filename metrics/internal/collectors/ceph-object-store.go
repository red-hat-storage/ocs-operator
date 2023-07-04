package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// component within the project/exporter
	rgwSubsystem = "rgw"
)

var _ prometheus.Collector = &CephObjectStoreCollector{}

// CephObjectStoreCollector is a custom collector for CephObjectStore Custom Resource
type CephObjectStoreCollector struct {
	RGWHealthStatus   *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephObjectStoreCollector constructs a collector
func NewCephObjectStoreCollector(opts *options.Options) *CephObjectStoreCollector {

	sharedIndexInformer := CephObjectStoreInformer(opts)
	if sharedIndexInformer == nil {
		return nil
	}

	return &CephObjectStoreCollector{
		RGWHealthStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, rgwSubsystem, "health_status"),
			`Health Status of RGW Endpoint. 0=Connected, 1=Progressing & 2=Failure`,
			[]string{"name", "namespace", "rgw_endpoint"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephObjectStore informer
func (c *CephObjectStoreCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephObjectStoreCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.RGWHealthStatus,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephObjectStoreCollector) Collect(ch chan<- prometheus.Metric) {
	cephObjectStoreLister := cephv1listers.NewCephObjectStoreLister(c.Informer.GetIndexer())
	cephObjectStores := getAllObjectStores(cephObjectStoreLister, c.AllowedNamespaces)

	if len(cephObjectStores) > 0 {
		c.collectObjectStoreHealth(cephObjectStores, ch)
	}
}

func getAllObjectStores(lister cephv1listers.CephObjectStoreLister, namespaces []string) (cephObjectStores []*cephv1.CephObjectStore) {
	var tempCephObjectStores []*cephv1.CephObjectStore
	var err error
	if len(namespaces) == 0 {
		cephObjectStores, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStores. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephObjectStores, err = lister.CephObjectStores(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStores in namespace %s. %v", namespace, err)
			continue
		}
		cephObjectStores = append(cephObjectStores, tempCephObjectStores...)
	}
	return
}

func (c *CephObjectStoreCollector) collectObjectStoreHealth(cephObjectStores []*cephv1.CephObjectStore, ch chan<- prometheus.Metric) {
	for _, cephObjectStore := range cephObjectStores {
		switch cephObjectStore.Status.Phase {
		case cephv1.ConditionConnected:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 0,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionProgressing:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 1,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionFailure:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 2,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionConnecting:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 3,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionReady:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 4,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionDeleting:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 5,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionDeletionIsBlocked:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 6,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		default:
			klog.Errorf("CephObjectStore in unexpected phase. Must be %q, %q or %q",
				cephv1.ConditionConnected, cephv1.ConditionProgressing, cephv1.ConditionFailure)
		}
	}
}

func CephObjectStoreInformer(opts *options.Options) cache.SharedIndexInformer {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephobjectstores", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephObjectStore{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return sharedIndexInformer
}
