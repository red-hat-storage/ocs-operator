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

const cephNVMeOFGatewaySubsystem = "cephnvmeofgateway"

var _ prometheus.Collector = &CephNVMeOFGatewayCollector{}

// CephNVMeOFGatewayCollector is a custom collector for CephNVMeOFGateway Custom Resource
type CephNVMeOFGatewayCollector struct {
	Phase             *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephNVMeOFGatewayCollector constructs a collector
func NewCephNVMeOFGatewayCollector(opts *options.Options) *CephNVMeOFGatewayCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephNVMeOFGateway collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephnvmeofgateways", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephNVMeOFGateway{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephNVMeOFGatewayCollector{
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephNVMeOFGatewaySubsystem, "status_phase"),
			`CephNVMeOFGateway Status Phase. 0=Empty, 1=Ready, 2=Failed, 3=Reconciling, 4=ReconcileFailed`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts the CephNVMeOFGateway informer
func (c *CephNVMeOFGatewayCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephNVMeOFGatewayCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Phase
}

// Collect implements prometheus.Collector interface
func (c *CephNVMeOFGatewayCollector) Collect(ch chan<- prometheus.Metric) {
	lister := cephv1listers.NewCephNVMeOFGatewayLister(c.Informer.GetIndexer())
	gateways := getAllCephNVMeOFGateways(lister, c.AllowedNamespaces)
	if len(gateways) > 0 {
		c.collectCephNVMeOFGatewayPhase(gateways, ch)
	}
}

func getAllCephNVMeOFGateways(lister cephv1listers.CephNVMeOFGatewayLister, namespaces []string) []*cephv1.CephNVMeOFGateway {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephNVMeOFGateways: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephNVMeOFGateway
	for _, ns := range namespaces {
		items, err := lister.CephNVMeOFGateways(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephNVMeOFGateways in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func (c *CephNVMeOFGatewayCollector) collectCephNVMeOFGatewayPhase(gateways []*cephv1.CephNVMeOFGateway, ch chan<- prometheus.Metric) {
	for _, gw := range gateways {
		var current string
		if gw.Status != nil {
			current = gw.Status.Phase
		}
		code, ok := rookGenericStatusCode(current)
		if !ok {
			klog.Errorf("CephNVMeOFGateway %q in unexpected phase %q. Must be one of %v", gw.Name, current, rookGenericPhases)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.Phase,
			prometheus.GaugeValue, code,
			gw.Name, gw.Namespace)
	}
}
