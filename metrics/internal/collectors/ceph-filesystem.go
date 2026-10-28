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

const (
	cephFilesystemSubsystem               = "cephfilesystem"
	cephFilesystemSubVolumeGroupSubsystem = "cephfilesystemsubvolumegroup"
)

// cephFilesystemPhases enumerates every cephv1.ConditionType value a CephFilesystem's
// status.phase can report, used to emit one-hot phase metrics.
var cephFilesystemPhases = []cephv1.ConditionType{
	cephv1.ConditionConnecting,
	cephv1.ConditionConnected,
	cephv1.ConditionProgressing,
	cephv1.ConditionReady,
	cephv1.ConditionFailure,
	cephv1.ConditionDeleting,
	cephv1.ConditionDeletionIsBlocked,
}

var _ prometheus.Collector = &CephFilesystemCollector{}

// CephFilesystemCollector is a custom collector for CephFilesystem and
// CephFilesystemSubVolumeGroup Custom Resources
type CephFilesystemCollector struct {
	Phase               *prometheus.Desc
	SubVolumeGroupPhase *prometheus.Desc
	Informer            cache.SharedIndexInformer
	InformerSvg         cache.SharedIndexInformer
	AllowedNamespaces   []string
}

// NewCephFilesystemCollector constructs a collector
func NewCephFilesystemCollector(opts *options.Options) *CephFilesystemCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephFilesystem collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephfilesystems", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephFilesystem{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	lwSvg := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephfilesystemsubvolumegroups", searchInNamespace(opts), fields.Everything())
	sharedIndexInformerSvg := cache.NewSharedIndexInformer(lwSvg, &cephv1.CephFilesystemSubVolumeGroup{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephFilesystemCollector{
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephFilesystemSubsystem, "status_phase"),
			`CephFilesystem Status Phase. One series per possible phase value with the active phase set to 1 and all others set to 0. Possible phases: Connecting, Connected, Progressing, Ready, Failure, Deleting, DeletionIsBlocked`,
			[]string{"name", "namespace", "phase"},
			nil,
		),
		SubVolumeGroupPhase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephFilesystemSubVolumeGroupSubsystem, "status_phase"),
			`CephFilesystemSubVolumeGroup Status Phase. 0=Connecting, 1=Connected, 2=Progressing, 3=Ready, 4=Failure, 5=Deleting, 6=DeletionIsBlocked`,
			[]string{"name", "namespace", "subvolume_group", "consumer_name"},
			nil,
		),
		Informer:          sharedIndexInformer,
		InformerSvg:       sharedIndexInformerSvg,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts the CephFilesystem and CephFilesystemSubVolumeGroup informers
func (c *CephFilesystemCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
	go c.InformerSvg.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephFilesystemCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.Phase,
		c.SubVolumeGroupPhase,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephFilesystemCollector) Collect(ch chan<- prometheus.Metric) {
	filesystemLister := cephv1listers.NewCephFilesystemLister(c.Informer.GetIndexer())
	filesystems := getAllCephFilesystems(filesystemLister, c.AllowedNamespaces)
	if len(filesystems) > 0 {
		c.collectCephFilesystemPhase(filesystems, ch)
	}

	svgLister := cephv1listers.NewCephFilesystemSubVolumeGroupLister(c.InformerSvg.GetIndexer())
	svgs := getAllCephFilesystemSubVolumeGroups(svgLister, c.AllowedNamespaces)
	if len(svgs) > 0 {
		c.collectCephFilesystemSubVolumeGroupPhase(svgs, ch)
	}
}

func getAllCephFilesystems(lister cephv1listers.CephFilesystemLister, namespaces []string) []*cephv1.CephFilesystem {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephFilesystems: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephFilesystem
	for _, ns := range namespaces {
		items, err := lister.CephFilesystems(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephFilesystems in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func getAllCephFilesystemSubVolumeGroups(lister cephv1listers.CephFilesystemSubVolumeGroupLister, namespaces []string) []*cephv1.CephFilesystemSubVolumeGroup {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephFilesystemSubVolumeGroups: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephFilesystemSubVolumeGroup
	for _, ns := range namespaces {
		items, err := lister.CephFilesystemSubVolumeGroups(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephFilesystemSubVolumeGroups in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func (c *CephFilesystemCollector) collectCephFilesystemPhase(filesystems []*cephv1.CephFilesystem, ch chan<- prometheus.Metric) {
	for _, fs := range filesystems {
		var current cephv1.ConditionType
		if fs.Status != nil {
			current = fs.Status.Phase
		}
		for _, phase := range cephFilesystemPhases {
			val := 0.0
			if phase == current {
				val = 1
			}
			ch <- prometheus.MustNewConstMetric(c.Phase,
				prometheus.GaugeValue, val,
				fs.Name, fs.Namespace, string(phase))
		}
	}
}

// collectCephFilesystemSubVolumeGroupPhase encodes the active phase as the metric value
// rather than emitting one series per phase: unlike CephFilesystem, the number of
// CephFilesystemSubVolumeGroup instances scales with the number of StorageConsumers, so a
// one-hot encoding would multiply the series count by len(cephFilesystemPhases) per consumer.
func (c *CephFilesystemCollector) collectCephFilesystemSubVolumeGroupPhase(svgs []*cephv1.CephFilesystemSubVolumeGroup, ch chan<- prometheus.Metric) {
	for _, svg := range svgs {
		var current cephv1.ConditionType
		if svg.Status != nil {
			current = svg.Status.Phase
		}
		groupName := svg.Spec.Name
		if groupName == "" {
			groupName = svg.Name
		}
		consumer := consumerOwnerName(svg.OwnerReferences)
		switch current {
		case cephv1.ConditionConnecting:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 0,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionConnected:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 1,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionProgressing:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 2,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionReady:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 3,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionFailure:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 4,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionDeleting:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 5,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		case cephv1.ConditionDeletionIsBlocked:
			ch <- prometheus.MustNewConstMetric(c.SubVolumeGroupPhase,
				prometheus.GaugeValue, 6,
				svg.Spec.FilesystemName, svg.Namespace, groupName, consumer)
		default:
			klog.Errorf("CephFilesystemSubVolumeGroup %q in unexpected phase %q. Must be one of %v", svg.Name, current, cephFilesystemPhases)
		}
	}
}
