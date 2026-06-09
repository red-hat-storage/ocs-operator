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
	mirrorDaemonSubsystem     = "mirror_daemon"
	lvmOSD                    = "lvm_osds"
	cephxKeyRotationSubsystem = "cephx_daemon_key_rotation"
)

// Daemon type constants for CephX key rotation
const (
	daemonMon = "mon"
	daemonMgr = "mgr"
	daemonOSD = "osd"
	daemonMDS = "mds"
)

var _ prometheus.Collector = &CephClusterCollector{}

// CephClusterCollector is a custom collector for CephCluster Custom Resource
type CephClusterCollector struct {
	MirrorDaemonCount      *prometheus.Desc
	LegacyOSD              *prometheus.Desc
	KeyRotationMismatch    *prometheus.Desc
	Informer               cache.SharedIndexInformer
	CephFilesystemInformer cache.SharedIndexInformer
	AllowedNamespaces      []string
}

// NewCephClusterCollector constructs a collector
func NewCephClusterCollector(opts *options.Options) *CephClusterCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
	}

	// CephCluster informer
	cephClusterLW := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephclusters", searchInNamespace(opts), fields.Everything())
	cephClusterInformer := cache.NewSharedIndexInformer(cephClusterLW, &cephv1.CephCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	// CephFilesystem informer (for MDS key rotation status)
	cephFilesystemLW := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephfilesystems", searchInNamespace(opts), fields.Everything())
	cephFilesystemInformer := cache.NewSharedIndexInformer(cephFilesystemLW, &cephv1.CephFilesystem{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

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
		KeyRotationMismatch: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephxKeyRotationSubsystem, "mismatch"),
			"Indicates if CephX key rotation has failed (1) or succeeded (0) for a daemon",
			[]string{"daemon", "ceph_cluster", "namespace"},
			nil,
		),
		Informer:               cephClusterInformer,
		CephFilesystemInformer: cephFilesystemInformer,
		AllowedNamespaces:      opts.AllowedNamespaces,
	}
}

// Run starts CephCluster and CephFilesystem informers
func (c *CephClusterCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
	go c.CephFilesystemInformer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephClusterCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirrorDaemonCount,
		c.LegacyOSD,
		c.KeyRotationMismatch,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephClusterCollector) Collect(ch chan<- prometheus.Metric) {
	cephClusterLister := cephv1listers.NewCephClusterLister(c.Informer.GetIndexer())
	cephFilesystemLister := cephv1listers.NewCephFilesystemLister(c.CephFilesystemInformer.GetIndexer())
	cephClusters := getAllCephClusters(cephClusterLister, c.AllowedNamespaces)

	if len(cephClusters) > 0 {
		c.collectMirrorinDaemonCount(cephClusters, ch)
		c.collectLegacyOSDCount(cephClusters, ch)
		c.collectCephxKeyRotationStatus(cephClusters, cephFilesystemLister, ch)
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

func (c *CephClusterCollector) collectCephxKeyRotationStatus(
	cephClusters []*cephv1.CephCluster,
	cephFilesystemLister cephv1listers.CephFilesystemLister,
	ch chan<- prometheus.Metric,
) {
	for _, cephCluster := range cephClusters {
		clusterName := cephCluster.Name
		clusterNamespace := cephCluster.Namespace

		// Check if status.KeyGeneration is behind spec.KeyGeneration for each daemon.
		// We only alert when status < spec (rotation incomplete/failed).
		// Status can legitimately exceed spec due to:
		//   - Greenfield clusters: spec defaults to 0, but Rook initializes status to 1
		//   - keyType changes: triggers rotation without changing keyGeneration spec
		//   - Other internal rotations
		desiredKeyGen := cephCluster.Spec.Security.CephX.Daemon.KeyGeneration
		cephxStatus := cephCluster.Status.Cephx

		// Check mon
		monMismatch := c.checkKeyGenMismatch(desiredKeyGen, cephxStatus.Mon.KeyGeneration)
		c.emitKeyRotationMetric(ch, daemonMon, clusterName, clusterNamespace, monMismatch)

		// Check mgr
		mgrMismatch := c.checkKeyGenMismatch(desiredKeyGen, cephxStatus.Mgr.KeyGeneration)
		c.emitKeyRotationMetric(ch, daemonMgr, clusterName, clusterNamespace, mgrMismatch)

		// Check osd
		osdMismatch := c.checkKeyGenMismatch(desiredKeyGen, cephxStatus.OSD.KeyGeneration)
		c.emitKeyRotationMetric(ch, daemonOSD, clusterName, clusterNamespace, osdMismatch)

		// Check mds via CephFilesystems
		mdsMismatch := c.checkMDSKeyRotation(cephFilesystemLister, clusterNamespace, desiredKeyGen)
		c.emitKeyRotationMetric(ch, daemonMDS, clusterName, clusterNamespace, mdsMismatch)
	}
}

func (c *CephClusterCollector) checkKeyGenMismatch(desired, actual uint32) float64 {
	// Alert only when status is behind spec (rotation incomplete/failed).
	// Status can legitimately exceed spec due to keyType changes or other rotations.
	if actual < desired {
		return 1
	}
	return 0
}

func (c *CephClusterCollector) checkMDSKeyRotation(
	lister cephv1listers.CephFilesystemLister,
	namespace string,
	desiredKeyGen uint32,
) float64 {
	filesystems, err := lister.CephFilesystems(namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list CephFilesystems in namespace %s: %v", namespace, err)
		return 0
	}
	if len(filesystems) == 0 {
		// No CephFilesystems, no mds to check
		return 0
	}

	// If ANY CephFilesystem has status behind spec, return 1
	for _, fs := range filesystems {
		actualKeyGen := fs.Status.Cephx.Daemon.KeyGeneration
		if actualKeyGen < desiredKeyGen {
			return 1
		}
	}
	return 0
}

func (c *CephClusterCollector) emitKeyRotationMetric(
	ch chan<- prometheus.Metric,
	daemon, clusterName, namespace string,
	value float64,
) {
	ch <- prometheus.MustNewConstMetric(
		c.KeyRotationMismatch,
		prometheus.GaugeValue,
		value,
		daemon,
		clusterName,
		namespace,
	)
}
