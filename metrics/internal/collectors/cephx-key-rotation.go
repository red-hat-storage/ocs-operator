package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	cephxKeyRotationSubsystem = "cephx_daemon_key_rotation"
)

// Daemon type constants
const (
	daemonMon = "mon"
	daemonMgr = "mgr"
	daemonOSD = "osd"
	daemonMDS = "mds"
)

var _ prometheus.Collector = &CephxKeyRotationCollector{}

// CephxKeyRotationCollector is a custom collector for monitoring CephX key rotation status
type CephxKeyRotationCollector struct {
	KeyRotationMismatch    *prometheus.Desc
	CephClusterInformer    cache.SharedIndexInformer
	CephFilesystemInformer cache.SharedIndexInformer
	AllowedNamespaces      []string
}

// NewCephxKeyRotationCollector constructs a collector for CephX key rotation monitoring
func NewCephxKeyRotationCollector(opts *options.Options) *CephxKeyRotationCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client for CephxKeyRotationCollector: %v", err)
		return nil
	}

	// Create CephCluster informer
	cephClusterLW := cache.NewListWatchFromClient(
		client.CephV1().RESTClient(),
		"cephclusters",
		searchInNamespace(opts),
		fields.Everything(),
	)
	cephClusterInformer := cache.NewSharedIndexInformer(
		cephClusterLW,
		&cephv1.CephCluster{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	// Create CephFilesystem informer
	cephFilesystemLW := cache.NewListWatchFromClient(
		client.CephV1().RESTClient(),
		"cephfilesystems",
		searchInNamespace(opts),
		fields.Everything(),
	)
	cephFilesystemInformer := cache.NewSharedIndexInformer(
		cephFilesystemLW,
		&cephv1.CephFilesystem{},
		0,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
	)

	return &CephxKeyRotationCollector{
		KeyRotationMismatch: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephxKeyRotationSubsystem, "mismatch"),
			"Indicates if CephX key rotation has failed (1) or succeeded (0) for a daemon",
			[]string{"daemon", "ceph_cluster", "namespace"},
			nil,
		),
		CephClusterInformer:    cephClusterInformer,
		CephFilesystemInformer: cephFilesystemInformer,
		AllowedNamespaces:      opts.AllowedNamespaces,
	}
}

// Run starts the CephCluster and CephFilesystem informers
func (c *CephxKeyRotationCollector) Run(stopCh <-chan struct{}) {
	go c.CephClusterInformer.Run(stopCh)
	go c.CephFilesystemInformer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephxKeyRotationCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.KeyRotationMismatch
}

// Collect implements prometheus.Collector interface
func (c *CephxKeyRotationCollector) Collect(ch chan<- prometheus.Metric) {
	cephClusterLister := cephv1listers.NewCephClusterLister(c.CephClusterInformer.GetIndexer())
	cephFilesystemLister := cephv1listers.NewCephFilesystemLister(c.CephFilesystemInformer.GetIndexer())

	cephClusters := c.getAllCephClusters(cephClusterLister)
	if len(cephClusters) == 0 {
		return
	}

	for _, cephCluster := range cephClusters {
		c.collectKeyRotationStatus(cephCluster, cephFilesystemLister, ch)
	}
}

func (c *CephxKeyRotationCollector) getAllCephClusters(lister cephv1listers.CephClusterLister) []*cephv1.CephCluster {
	var cephClusters []*cephv1.CephCluster
	var err error

	if len(c.AllowedNamespaces) == 0 {
		cephClusters, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters: %v", err)
		}
		return cephClusters
	}

	for _, namespace := range c.AllowedNamespaces {
		clusters, err := lister.CephClusters(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephClusters in namespace %s: %v", namespace, err)
			continue
		}
		cephClusters = append(cephClusters, clusters...)
	}
	return cephClusters
}

func (c *CephxKeyRotationCollector) getCephFilesystemsForCluster(
	lister cephv1listers.CephFilesystemLister,
	namespace string,
) []*cephv1.CephFilesystem {
	filesystems, err := lister.CephFilesystems(namespace).List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list CephFilesystems in namespace %s: %v", namespace, err)
		return nil
	}
	return filesystems
}

func (c *CephxKeyRotationCollector) collectKeyRotationStatus(
	cephCluster *cephv1.CephCluster,
	cephFilesystemLister cephv1listers.CephFilesystemLister,
	ch chan<- prometheus.Metric,
) {
	clusterName := cephCluster.Name
	clusterNamespace := cephCluster.Namespace

	// GreenField cluster: has the CreatedWithCephXFeaturesAnnotationKey annotation
	// Keys are created fresh with aes256k, no rotation needed
	if _, ok := cephCluster.GetAnnotations()[util.CreatedWithCephXFeaturesAnnotationKey]; ok {
		c.emitMetric(ch, daemonMon, clusterName, clusterNamespace, 0)
		c.emitMetric(ch, daemonMgr, clusterName, clusterNamespace, 0)
		c.emitMetric(ch, daemonOSD, clusterName, clusterNamespace, 0)
		c.emitMetric(ch, daemonMDS, clusterName, clusterNamespace, 0)
		return
	}

	// BrownField cluster: compare spec vs status for each daemon
	desiredKeyGen := cephCluster.Spec.Security.CephX.Daemon.KeyGeneration
	cephxStatus := cephCluster.Status.Cephx

	// Check mon
	monMismatch := c.checkMismatch(desiredKeyGen, cephxStatus.Mon.KeyGeneration)
	c.emitMetric(ch, daemonMon, clusterName, clusterNamespace, monMismatch)

	// Check mgr
	mgrMismatch := c.checkMismatch(desiredKeyGen, cephxStatus.Mgr.KeyGeneration)
	c.emitMetric(ch, daemonMgr, clusterName, clusterNamespace, mgrMismatch)

	// Check osd
	osdMismatch := c.checkMismatch(desiredKeyGen, cephxStatus.OSD.KeyGeneration)
	c.emitMetric(ch, daemonOSD, clusterName, clusterNamespace, osdMismatch)

	// Check mds via CephFilesystems
	mdsMismatch := c.checkMDSKeyRotation(cephFilesystemLister, clusterNamespace, desiredKeyGen)
	c.emitMetric(ch, daemonMDS, clusterName, clusterNamespace, mdsMismatch)
}

func (c *CephxKeyRotationCollector) checkMismatch(desired, actual uint32) float64 {
	if desired != actual {
		return 1
	}
	return 0
}

func (c *CephxKeyRotationCollector) checkMDSKeyRotation(
	lister cephv1listers.CephFilesystemLister,
	namespace string,
	desiredKeyGen uint32,
) float64 {
	filesystems := c.getCephFilesystemsForCluster(lister, namespace)
	if len(filesystems) == 0 {
		// No CephFilesystems, no mds to check
		return 0
	}

	// If ANY CephFilesystem has a mismatch, return 1
	for _, fs := range filesystems {
		actualKeyGen := fs.Status.Cephx.Daemon.KeyGeneration
		if desiredKeyGen != actualKeyGen {
			return 1
		}
	}
	return 0
}

func (c *CephxKeyRotationCollector) emitMetric(
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
