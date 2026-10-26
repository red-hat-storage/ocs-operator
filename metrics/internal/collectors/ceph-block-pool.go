package collectors

import (
	"strings"

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

const (
	poolMirroringSubsystem           = "pool_mirroring"
	blockPoolSubsystem               = "cephblockpool"
	blockPoolRadosNamespaceSubsystem = "cephblockpoolradosnamespace"
	defaultRadosNamespace            = "internal"
)

// cephBlockPoolPhases enumerates every cephv1.ConditionType value a CephBlockPool's
// status.phase can report.  This is used to generate one series per phase value with
// the active phase set to 1 and all others set to 0.
var cephBlockPoolPhases = []cephv1.ConditionType{
	cephv1.ConditionConnecting,
	cephv1.ConditionConnected,
	cephv1.ConditionProgressing,
	cephv1.ConditionReady,
	cephv1.ConditionFailure,
	cephv1.ConditionDeleting,
	cephv1.ConditionDeletionIsBlocked,
}

// imageHealthValue maps a Ceph image health string to its numeric gauge value.
// Returns -1 if the health string isn't recognized.
func imageHealthValue(health string) float64 {
	switch health {
	case "OK":
		return 0
	case "UNKNOWN":
		return 1
	case "WARNING":
		return 2
	case "ERROR":
		return 3
	default:
		return -1
	}
}

func boolToFloat64(b bool) float64 {
	if b {
		return 1
	}
	return 0
}

var _ prometheus.Collector = &CephBlockPoolCollector{}

// CephBlockPoolCollector is a custom collector for CephBlockPool Custom Resource
type CephBlockPoolCollector struct {
	MirroringImageHealth *prometheus.Desc
	MirroringStatus      *prometheus.Desc
	Phase                *prometheus.Desc
	RadosNamespacePhase  *prometheus.Desc
	Informer             cache.SharedIndexInformer
	InformerRs           cache.SharedIndexInformer
	AllowedNamespaces    []string
}

// NewCephBlockPoolCollector constructs a collector
func NewCephBlockPoolCollector(opts *options.Options) *CephBlockPoolCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephBlockPool collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephblockpools", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephBlockPool{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	lwRs := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephblockpoolradosnamespaces", searchInNamespace(opts), fields.Everything())
	sharedIndexInformerRs := cache.NewSharedIndexInformer(lwRs, &cephv1.CephBlockPoolRadosNamespace{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephBlockPoolCollector{
		MirroringImageHealth: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, poolMirroringSubsystem, "image_health"),
			`Pool Mirroring Image Health. 0=OK, 1=UNKNOWN, 2=WARNING & 3=ERROR`,
			[]string{"name", "namespace", "rados_namespace", "consumer_name"},
			nil,
		),
		MirroringStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, poolMirroringSubsystem, "status"),
			`Pool Mirroring Status.  0=Disabled, 1=Enabled`,
			[]string{"name", "namespace", "rados_namespace", "consumer_name"},
			nil,
		),
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, blockPoolSubsystem, "status_phase"),
			`CephBlockPool Status Phase. One series per possible phase value with the active phase set to 1 and all others set to 0. Possible phases: Connecting, Connected, Progressing, Ready, Failure, Deleting, DeletionIsBlocked`,
			[]string{"name", "namespace", "phase"},
			nil,
		),
		RadosNamespacePhase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, blockPoolRadosNamespaceSubsystem, "status_phase"),
			`CephBlockPoolRadosNamespace Status Phase. 0=Connecting, 1=Connected, 2=Progressing, 3=Ready, 4=Failure, 5=Deleting, 6=DeletionIsBlocked`,
			[]string{"name", "namespace", "rados_namespace", "consumer_name"},
			nil,
		),
		Informer:          sharedIndexInformer,
		InformerRs:        sharedIndexInformerRs,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephBlockPool informer
func (c *CephBlockPoolCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
	go c.InformerRs.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephBlockPoolCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirroringImageHealth,
		c.MirroringStatus,
		c.Phase,
		c.RadosNamespacePhase,
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
		c.collectCephBlockPoolPhase(cephBlockPools, ch)
	}

	radosNamespaceLister := cephv1listers.NewCephBlockPoolRadosNamespaceLister(c.InformerRs.GetIndexer())
	radosNamespaces := getAllBlockPoolsNamespaces(radosNamespaceLister, c.AllowedNamespaces)
	if len(radosNamespaces) > 0 {
		c.collectMirroringImageHealthRadosNamespace(radosNamespaces, ch)
		c.collectMirroringStatusRadosNamespace(radosNamespaces, ch)
		c.collectCephBlockPoolRadosNamespacePhase(radosNamespaces, ch)
	}
}

func getAllBlockPoolsNamespaces(lister cephv1listers.CephBlockPoolRadosNamespaceLister, namespaces []string) []*cephv1.CephBlockPoolRadosNamespace {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPoolRadosNamespaces: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephBlockPoolRadosNamespace
	for _, ns := range namespaces {
		items, err := lister.CephBlockPoolRadosNamespaces(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPoolRadosNamespaces in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func getAllBlockPools(lister cephv1listers.CephBlockPoolLister, namespaces []string) []*cephv1.CephBlockPool {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPools: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephBlockPool
	for _, ns := range namespaces {
		pools, err := lister.CephBlockPools(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephBlockPools in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, pools...)
	}
	return result
}

func (c *CephBlockPoolCollector) collectMirroringImageHealth(cephBlockPools []*cephv1.CephBlockPool, ch chan<- prometheus.Metric) {
	for _, cephBlockPool := range cephBlockPools {
		if !cephBlockPool.Spec.Mirroring.Enabled {
			continue
		}

		mirroringStatus := cephBlockPool.Status.MirroringStatus
		if mirroringStatus == nil || mirroringStatus.Summary == nil || len(strings.TrimSpace(mirroringStatus.Summary.ImageHealth)) == 0 {
			klog.Errorf("Mirroring is enabled on CephBlockPool %q but image health status is not available.", cephBlockPool.Name)
			continue
		}

		health := mirroringStatus.Summary.ImageHealth
		val := imageHealthValue(health)
		if val < 0 {
			klog.Errorf("Invalid image health %q for pool %s. Must be OK, UNKNOWN, WARNING or ERROR.", health, cephBlockPool.Name)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
			prometheus.GaugeValue, val,
			cephBlockPool.Name, cephBlockPool.Namespace, defaultRadosNamespace, "")
	}
}

func (c *CephBlockPoolCollector) collectMirroringStatus(cephBlockPools []*cephv1.CephBlockPool, ch chan<- prometheus.Metric) {
	for _, cephBlockPool := range cephBlockPools {
		ch <- prometheus.MustNewConstMetric(c.MirroringStatus,
			prometheus.GaugeValue, boolToFloat64(cephBlockPool.Spec.Mirroring.Enabled),
			cephBlockPool.Name, cephBlockPool.Namespace, defaultRadosNamespace, "")
	}
}

func (c *CephBlockPoolCollector) collectCephBlockPoolPhase(cephBlockPools []*cephv1.CephBlockPool, ch chan<- prometheus.Metric) {
	for _, cephBlockPool := range cephBlockPools {
		var current cephv1.ConditionType
		if cephBlockPool.Status != nil {
			current = cephBlockPool.Status.Phase
		}
		for _, phase := range cephBlockPoolPhases {
			val := 0.0
			if phase == current {
				val = 1
			}
			ch <- prometheus.MustNewConstMetric(c.Phase,
				prometheus.GaugeValue, val,
				cephBlockPool.Name, cephBlockPool.Namespace, string(phase))
		}
	}
}

func (c *CephBlockPoolCollector) collectMirroringImageHealthRadosNamespace(radosNamespaces []*cephv1.CephBlockPoolRadosNamespace, ch chan<- prometheus.Metric) {
	for _, rns := range radosNamespaces {
		if rns.Spec.Mirroring == nil {
			continue
		}
		mirroringStatus := rns.Status.MirroringStatus
		if mirroringStatus == nil || mirroringStatus.Summary == nil || len(strings.TrimSpace(mirroringStatus.Summary.ImageHealth)) == 0 {
			klog.Errorf("Mirroring is enabled on CephBlockPoolRadosNamespace %q but image health status is not available.", rns.Name)
			continue
		}

		health := mirroringStatus.Summary.ImageHealth
		val := imageHealthValue(health)
		if val < 0 {
			klog.Errorf("Invalid image health %q for rados namespace %s in pool %s. Must be OK, UNKNOWN, WARNING or ERROR.", health, rns.Name, rns.Spec.BlockPoolName)
			continue
		}
		consumer := consumerOwnerName(rns.OwnerReferences)
		ch <- prometheus.MustNewConstMetric(c.MirroringImageHealth,
			prometheus.GaugeValue, val,
			rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
	}
}

func (c *CephBlockPoolCollector) collectMirroringStatusRadosNamespace(radosNamespaces []*cephv1.CephBlockPoolRadosNamespace, ch chan<- prometheus.Metric) {
	for _, rns := range radosNamespaces {
		consumer := consumerOwnerName(rns.OwnerReferences)
		ch <- prometheus.MustNewConstMetric(c.MirroringStatus,
			prometheus.GaugeValue, boolToFloat64(rns.Spec.Mirroring != nil),
			rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
	}
}

func (c *CephBlockPoolCollector) collectCephBlockPoolRadosNamespacePhase(radosNamespaces []*cephv1.CephBlockPoolRadosNamespace, ch chan<- prometheus.Metric) {
	for _, rns := range radosNamespaces {
		var current cephv1.ConditionType
		if rns.Status != nil {
			current = rns.Status.Phase
		}
		consumer := consumerOwnerName(rns.OwnerReferences)
		switch current {
		case cephv1.ConditionConnecting:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 0,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionConnected:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 1,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionProgressing:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 2,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionReady:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 3,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionFailure:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 4,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionDeleting:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 5,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		case cephv1.ConditionDeletionIsBlocked:
			ch <- prometheus.MustNewConstMetric(c.RadosNamespacePhase,
				prometheus.GaugeValue, 6,
				rns.Spec.BlockPoolName, rns.Namespace, rns.Name, consumer)
		default:
			klog.Errorf("CephBlockPoolRadosNamespace %q in unexpected phase %q. Must be one of %v", rns.Name, current, cephBlockPoolPhases)
		}
	}
}
