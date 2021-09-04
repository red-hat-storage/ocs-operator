package collectors

import (
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	"github.com/prometheus/client_golang/prometheus"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	// component within the project/exporter
	mirrorDaemonSubsystem = "mirror_daemon"
)

var _ prometheus.Collector = &CephRBDMirrorCollector{}

// CephRBDMirrorCollector is a custom collector for CephRBDMirror Custom Resource
type CephRBDMirrorCollector struct {
	MirrorDaemonStatus *prometheus.Desc
	Informer           cache.SharedIndexInformer
	AllowedNamespaces  []string
}

// NewCephRBDMirrorCollector constructs a collector
func NewCephRBDMirrorCollector(opts *options.Options) *CephRBDMirrorCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephrbdmirrors", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephRBDMirror{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephRBDMirrorCollector{
		MirrorDaemonStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, mirrorDaemonSubsystem, "status"),
			`Mirror Daemon Status. 0=Ready, 1=Created & 2=Failed`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephRBDMirrors informer
func (c *CephRBDMirrorCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephRBDMirrorCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirrorDaemonStatus,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephRBDMirrorCollector) Collect(ch chan<- prometheus.Metric) {
	cephRBDMirrorLister := cephv1listers.NewCephRBDMirrorLister(c.Informer.GetIndexer())
	cephRBDMirrors := getAllRBDMirrors(cephRBDMirrorLister, c.AllowedNamespaces)

	if len(cephRBDMirrors) > 0 {
		c.collectMirrorinDaemonStatus(cephRBDMirrors, ch)
	}
}

func getAllRBDMirrors(lister cephv1listers.CephRBDMirrorLister, namespaces []string) (cephRBDMirrors []*cephv1.CephRBDMirror) {
	var tempCephRBDMirrors []*cephv1.CephRBDMirror
	var err error
	if len(namespaces) == 0 {
		cephRBDMirrors, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephRBDMirrors. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephRBDMirrors, err = lister.CephRBDMirrors(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephRBDMirror in namespace %s. %v", namespace, err)
			continue
		}
		cephRBDMirrors = append(cephRBDMirrors, tempCephRBDMirrors...)
	}
	return
}

func (c *CephRBDMirrorCollector) collectMirrorinDaemonStatus(cephRBDMirrors []*cephv1.CephRBDMirror, ch chan<- prometheus.Metric) {
	for _, cephRBDMirror := range cephRBDMirrors {
		switch cephRBDMirror.Status.Phase {
		case "Ready":
			ch <- prometheus.MustNewConstMetric(c.MirrorDaemonStatus,
				prometheus.GaugeValue, 0,
				cephRBDMirror.Name,
				cephRBDMirror.Namespace)
		case "Created":
			ch <- prometheus.MustNewConstMetric(c.MirrorDaemonStatus,
				prometheus.GaugeValue, 1,
				cephRBDMirror.Name,
				cephRBDMirror.Namespace)
		case "Failed":
			ch <- prometheus.MustNewConstMetric(c.MirrorDaemonStatus,
				prometheus.GaugeValue, 2,
				cephRBDMirror.Name,
				cephRBDMirror.Namespace)
		default:
			klog.Errorf("Mirroring daemon in unexpected status. Must be Ready, Created or Failed")
		}
	}
}
