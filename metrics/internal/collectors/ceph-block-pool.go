package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// component within the project/exporter
	poolMirroringSubsystem = "pool_mirroring"
)

var _ prometheus.Collector = &CephBlockPoolCollector{}

// CephBlockPoolCollector is a custom collector for CephBlockPool Custom Resource
type CephBlockPoolCollector struct {
	MirroringImageHealth *prometheus.Desc
	MirroringStatus      *prometheus.Desc
	Informer             cache.SharedIndexInformer
	AllowedNamespaces    []string
}

// NewCephBlockPoolCollector constructs a collector
func NewCephBlockPoolCollector(opts *options.Options) *CephBlockPoolCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephblockpools", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephBlockPool{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephBlockPoolCollector{
		MirroringImageHealth: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, poolMirroringSubsystem, "image_health"),
			`Pool Mirroring Image Health. 0=OK, 1=UNKNOWN, 2=WARNING & 3=ERROR`,
			[]string{"name", "namespace"},
			nil,
		),
		MirroringStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, poolMirroringSubsystem, "status"),
			`Pool Mirroring Status.  0=Disabled, 1=Enabled`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephBlockPool informer
func (c *CephBlockPoolCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephBlockPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirroringImageHealth,
		c.MirroringStatus,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephBlockPoolCollector) Collect(ch chan<- prometheus.Metric) {
	cephBlockPoolLister := cephv1listers.NewCephBlockPoolLister(c.Informer.GetIndexer())
	cephBlockPools := getAllBlockPools(cephBlockPoolLister, c.AllowedNamespaces)

	if len(cephBlockPools) > 0 {
		c.collectMirroringImageHealth(cephBlockPools, ch)
		c.collectMirroringStatus(cephBlockPools, ch)
	}
}

func getAllBlockPools(lister cephv1listers.CephBlockPoolLister, namespaces []string) (cephBlockPools []*cephv1.CephBlockPool) {
	var tempCephBlockPools []*cephv1.CephBlockPool
	var err error
	if len(namespaces) == 0 {
		cephBlockPools, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPools. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephBlockPools, err = lister.CephBlockPools(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPool in namespace %s. %v", namespace, err)
			continue
		}
		cephBlockPools = append(cephBlockPools, tempCephBlockPools...)
	}
	return
}

func (c *CephBlockPoolCollector) collectMirroringImageHealth(cephBlockPools []*cephv1.CephBlockPool, ch chan<- prometheus.Metric) {
	for _, cephBlockPool := range cephBlockPools {
		var imageHealth string
		mirroringStatus := cephBlockPool.Status.MirroringStatus
		if mirroringStatus != nil {
			imageHealth = mirroringStatus.Summary.ImageHealth
		}
		switch imageHealth {
		case "OK":
			ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
				prometheus.GaugeValue, 0,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		case "UNKNOWN":
			ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
				prometheus.GaugeValue, 1,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		case "WARNING":
			ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
				prometheus.GaugeValue, 2,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		case "ERROR":
			ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
				prometheus.GaugeValue, 3,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		default:
			klog.Errorf("Invalid image health, %q, for pool %s. Must be OK, UNKNOWN, WARNING or ERROR.", imageHealth, cephBlockPool.Name)
		}
	}
}

func (c *CephBlockPoolCollector) collectMirroringStatus(cephBlockPools []*cephv1.CephBlockPool, ch chan<- prometheus.Metric) {
	for _, cephBlockPool := range cephBlockPools {
		switch cephBlockPool.Spec.Mirroring.Enabled {
		case true:
			ch <- prometheus.MustNewConstMetric(c.MirroringStatus,
				prometheus.GaugeValue, 1,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		case false:
			ch <- prometheus.MustNewConstMetric(c.MirroringStatus,
				prometheus.GaugeValue, 0,
				cephBlockPool.Name,
				cephBlockPool.Namespace)
		default:
			klog.Errorf("Invalid spec for pool %s. CephBlockPool.Spec.Mirroring.Enabled must be true or false", cephBlockPool.Name)
		}
	}
}
