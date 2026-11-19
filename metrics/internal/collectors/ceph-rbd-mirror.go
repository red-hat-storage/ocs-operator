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

const cephRBDMirrorSubsystem = "cephrbdmirror"

var _ prometheus.Collector = &CephRBDMirrorCollector{}

// CephRBDMirrorCollector is a custom collector for CephRBDMirror Custom Resource
type CephRBDMirrorCollector struct {
	Phase             *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephRBDMirrorCollector constructs a collector
func NewCephRBDMirrorCollector(opts *options.Options) *CephRBDMirrorCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephRBDMirror collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephrbdmirrors", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephRBDMirror{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephRBDMirrorCollector{
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephRBDMirrorSubsystem, "status_phase"),
			`CephRBDMirror Status Phase. 0=Empty, 1=Ready, 2=Failed, 3=Reconciling, 4=ReconcileFailed`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts the CephRBDMirror informer
func (c *CephRBDMirrorCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephRBDMirrorCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Phase
}

// Collect implements prometheus.Collector interface
func (c *CephRBDMirrorCollector) Collect(ch chan<- prometheus.Metric) {
	lister := cephv1listers.NewCephRBDMirrorLister(c.Informer.GetIndexer())
	mirrors := getAllCephRBDMirrors(lister, c.AllowedNamespaces)
	if len(mirrors) > 0 {
		c.collectCephRBDMirrorPhase(mirrors, ch)
	}
}

func getAllCephRBDMirrors(lister cephv1listers.CephRBDMirrorLister, namespaces []string) []*cephv1.CephRBDMirror {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephRBDMirrors: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephRBDMirror
	for _, ns := range namespaces {
		items, err := lister.CephRBDMirrors(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephRBDMirrors in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func (c *CephRBDMirrorCollector) collectCephRBDMirrorPhase(mirrors []*cephv1.CephRBDMirror, ch chan<- prometheus.Metric) {
	for _, mirror := range mirrors {
		var current string
		if mirror.Status != nil {
			current = mirror.Status.Phase
		}
		code, ok := rookGenericStatusCode(current)
		if !ok {
			klog.Errorf("CephRBDMirror %q in unexpected phase %q. Must be one of %v", mirror.Name, current, rookGenericPhases)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.Phase,
			prometheus.GaugeValue, code,
			mirror.Name, mirror.Namespace)
	}
}
