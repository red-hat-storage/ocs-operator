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
	// component within the project/exporter
	mirrorDaemonSubsystem = "mirror_daemon"
	lvmOSD                = "lvm_osds"
)

var _ prometheus.Collector = &CephClusterCollector{}

// CephClusterCollector is a custom collector for CephCluster Custom Resource
type CephClusterCollector struct {
	MirrorDaemonCount *prometheus.Desc
	LegacyOSD         *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephClusterCollector constructs a collector
func NewCephClusterCollector(opts *options.Options) *CephClusterCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephclusters", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephClusterCollector{
		MirrorDaemonCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, mirrorDaemonSubsystem, "count"),
			`Mirror Daemon Count.`,
			[]string{"ceph_cluster", "namespace"},
			nil,
		),
		LegacyOSD: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, lvmOSD, "count"),
			`Number of OSDs on LVM`,
			[]string{"ceph_cluster", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts CephClusters informer
func (c *CephClusterCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirrorDaemonCount,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephClusterCollector) Collect(ch chan<- prometheus.Metric) {
	cephClusterLister := cephv1listers.NewCephClusterLister(c.Informer.GetIndexer())
	cephClusterListers := getAllCephClusters(cephClusterLister, c.AllowedNamespaces)

	if len(cephClusterListers) > 0 {
		c.collectMirrorinDaemonCount(cephClusterListers, ch)
		c.collectLegacyOSDCount(cephClusterListers, ch)
	}
}

func getAllCephClusters(lister cephv1listers.CephClusterLister, namespaces []string) (cephClusters []*cephv1.CephCluster) {
	var tempCephClusters []*cephv1.CephCluster
	var err error
	if len(namespaces) == 0 {
		cephClusters, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephClusters, err = lister.CephClusters(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters in namespace %s. %v", namespace, err)
			continue
		}
		cephClusters = append(cephClusters, tempCephClusters...)
	}
	return
}

func (c *CephClusterCollector) collectMirrorinDaemonCount(cephClusters []*cephv1.CephCluster, ch chan<- prometheus.Metric) {
	for _, cephCluster := range cephClusters {
		cephStatus := cephCluster.Status.CephStatus
		if cephStatus != nil && cephStatus.Versions != nil && cephStatus.Versions.RbdMirror != nil {
			daemonCount := 0
			for _, count := range cephStatus.Versions.RbdMirror {
				daemonCount += count
			}
			ch <- prometheus.MustNewConstMetric(c.MirrorDaemonCount,
				prometheus.GaugeValue, float64(daemonCount),
				cephCluster.Name,
				cephCluster.Namespace)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.MirrorDaemonCount,
			prometheus.GaugeValue, 0,
			cephCluster.Name,
			cephCluster.Namespace)
	}
}

func (c *CephClusterCollector) collectLegacyOSDCount(cephClusters []*cephv1.CephCluster, ch chan<- prometheus.Metric) {
	for _, cephCluster := range cephClusters {
		cephStorage := cephCluster.Status.CephStorage
		legacyOSDCount := 0
		reason := "LVM-based OSDs on a PVC are deprecated, see documentation on replacing OSDs"
		if cephStorage != nil && cephStorage.DeprecatedOSDs != nil {
			legacyOSDCount += len(cephStorage.DeprecatedOSDs[reason])
			ch <- prometheus.MustNewConstMetric(c.LegacyOSD,
				prometheus.GaugeValue, float64(legacyOSDCount),
				cephCluster.Name,
				cephCluster.Namespace)
		}
	}
}
