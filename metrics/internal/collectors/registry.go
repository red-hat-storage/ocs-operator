package collectors

import (
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

const (
	namespace     = "ocs"
	pvMetadataKey = "csi.storage.k8s.io/pv/name"
)

func searchInNamespace(opts *options.Options) string {
	if opts != nil && len(opts.AllowedNamespaces) == 1 {
		return opts.AllowedNamespaces[0]
	}
	return metav1.NamespaceAll
}

var (
	cephScannersExpected atomic.Int32
	cephScannersReady    atomic.Int32
	scanWg               sync.WaitGroup
)

// CephReady reports whether all registered Ceph scanners have completed
// their first scan.
func CephReady() bool {
	expected := cephScannersExpected.Load()
	return expected > 0 && cephScannersReady.Load() >= expected
}

// WaitForScans blocks until all scan goroutines started by runScanLoop exit.
func WaitForScans() {
	scanWg.Wait()
}

func registerCephScanner() {
	cephScannersExpected.Add(1)
}

// scanRetryInterval controls the delay between retries when the initial scan
// fails. Tests can override this for faster execution.
var scanRetryInterval = 30 * time.Second

// runScanLoop starts a goroutine that retries the initial scan until it
// succeeds, then loops on a ticker. The scan function returns true on
// success, false on failure.
func runScanLoop(stopCh <-chan struct{}, interval time.Duration, scan func() bool) {
	scanWg.Add(1)
	go func() {
		defer scanWg.Done()
		for !scan() {
			select {
			case <-stopCh:
				return
			case <-time.After(scanRetryInterval):
			}
		}
		cephScannersReady.Add(1)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				return
			case <-ticker.C:
				scan()
			}
		}
	}()
}

func consumerOwnerName(refs []metav1.OwnerReference) string {
	for _, ref := range refs {
		if ref.Kind == "StorageConsumer" {
			return ref.Name
		}
	}
	return ""
}

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry.
func RegisterCustomResourceCollectors(registry *prometheus.Registry, opts *options.Options) {
	cephObjectStoreCollector := NewCephObjectStoreCollector(opts)
	cephClusterCollector := NewCephClusterCollector(opts)
	OBMetricsCollector := NewObjectBucketCollector(opts)

	cephObjectStoreCollector.Run(opts.StopCh)
	cephClusterCollector.Run(opts.StopCh)
	OBMetricsCollector.Run(opts.StopCh)
	registry.MustRegister(
		cephObjectStoreCollector,
		cephClusterCollector,
		OBMetricsCollector,
	)

	if c := NewCephBlockPoolCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewClusterAdvancedFeatureCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewStorageConsumerCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewStorageClusterCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewStorageAutoScalerCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewOperatorConditionCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewCephxKeyRotationCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}

	healthScoreCollector, healthScoreErr := NewHealthScoreCollector(opts)
	if healthScoreErr != nil {
		klog.Errorf("Health score collector not registered: %v", healthScoreErr)
	} else if healthScoreCollector != nil {
		healthScoreCollector.Run(opts.StopCh)
		registry.MustRegister(healthScoreCollector)
	}
}

// RegisterNonCephCollectors registers only the collectors that don't
// depend on Ceph CRDs or tooling. Used in external mode and NooBaa standalone.
func RegisterNonCephCollectors(registry *prometheus.Registry, opts *options.Options) {
	if c := NewOperatorConditionCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
	if c := NewStorageClusterCollector(opts); c != nil {
		c.Run(opts.StopCh)
		registry.MustRegister(c)
	}
}

func RegisterCephRBDCollector(registry *prometheus.Registry, conn *cephconn.Conn, opts *options.Options) *CephRBDCollector {
	if len(opts.AllowedNamespaces) == 0 {
		klog.Error("CephRBD collector not registered: no allowed namespaces")
		return nil
	}
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("CephRBD collector not registered: %v", err)
		return nil
	}
	dynClient, err := dynamic.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Warningf("dynamic client not available, blocklist scan disabled: %v", err)
	}
	registerCephScanner()
	rbdCollector := NewCephRBDCollector(conn, client, dynClient, opts.AllowedNamespaces[0], opts.ScanInterval)
	rbdCollector.Run(opts.StopCh)
	registry.MustRegister(rbdCollector)
	return rbdCollector
}

// RegisterCephBlocklistCollector registers the Ceph blocklist collector.
// It reads blocklist data from the RBD collector's scan loop.
func RegisterCephBlocklistCollector(registry *prometheus.Registry, rbdCollector *CephRBDCollector) {
	if rbdCollector == nil {
		klog.Error("CephBlocklist collector not registered: RBD collector not available")
		return
	}
	blocklistCollector := NewCephBlocklistCollector(rbdCollector)
	registry.MustRegister(blocklistCollector)
}

func RegisterCephFSMetricsCollector(registry *prometheus.Registry, conn *cephconn.Conn, opts *options.Options) {
	if len(opts.AllowedNamespaces) == 0 {
		klog.Error("CephFS collector not registered: no allowed namespaces")
		return
	}
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("CephFS collector not registered: %v", err)
		return
	}
	registerCephScanner()
	cephFSCollector := NewCephFSSubvolumeCountCollector(conn, client, opts.AllowedNamespaces[0], opts.ScanInterval)
	cephFSCollector.Run(opts.StopCh)
	registry.MustRegister(cephFSCollector)
}
