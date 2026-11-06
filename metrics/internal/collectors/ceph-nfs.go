package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const cephNFSSubsystem = "cephnfs"

var _ prometheus.Collector = &CephNFSCollector{}

// CephNFSCollector is a custom collector for CephNFS Custom Resource
type CephNFSCollector struct {
	Phase             *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephNFSCollector constructs a collector
func NewCephNFSCollector(opts *options.Options) *CephNFSCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephNFS collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephnfses", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephNFS{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephNFSCollector{
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephNFSSubsystem, "status_phase"),
			`CephNFS Status Phase. 0=Empty, 1=Ready, 2=Failed, 3=Reconciling, 4=ReconcileFailed`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts the CephNFS informer
func (c *CephNFSCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephNFSCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Phase
}

// Collect implements prometheus.Collector interface
func (c *CephNFSCollector) Collect(ch chan<- prometheus.Metric) {
	lister := cephv1listers.NewCephNFSLister(c.Informer.GetIndexer())
	nfses := getAllCephNFSes(lister, c.AllowedNamespaces)
	if len(nfses) > 0 {
		c.collectCephNFSPhase(nfses, ch)
	}
}

func getAllCephNFSes(lister cephv1listers.CephNFSLister, namespaces []string) []*cephv1.CephNFS {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephNFSes: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephNFS
	for _, ns := range namespaces {
		items, err := lister.CephNFSes(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephNFSes in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func (c *CephNFSCollector) collectCephNFSPhase(nfses []*cephv1.CephNFS, ch chan<- prometheus.Metric) {
	for _, nfs := range nfses {
		var current string
		if nfs.Status != nil {
			current = nfs.Status.Phase
		}
		code, ok := rookGenericStatusCode(current)
		if !ok {
			klog.Errorf("CephNFS %q in unexpected phase %q. Must be one of %v", nfs.Name, current, rookGenericPhases)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.Phase,
			prometheus.GaugeValue, code,
			nfs.Name, nfs.Namespace)
	}
}
