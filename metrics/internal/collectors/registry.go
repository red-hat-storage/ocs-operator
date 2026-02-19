package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	// name of the project/exporter
	namespace = "ocs"
)

func searchInNamespace(opts *options.Options) (returnNamespace string) {
	returnNamespace = metav1.NamespaceAll
	if opts != nil && len(opts.AllowedNamespaces) == 1 {
		returnNamespace = opts.AllowedNamespaces[0]
	}
	return
}

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(registry *prometheus.Registry, opts *options.Options) {
	cephObjectStoreCollector := NewCephObjectStoreCollector(opts)
	cephBlockPoolCollector := NewCephBlockPoolCollector(opts)
	cephClusterCollector := NewCephClusterCollector(opts)
	storageClusterCollector := NewStorageClusterCollector(opts)
	storageAutoScalerCollector := NewStorageAutoScalerCollector(opts)
	OBMetricsCollector := NewObjectBucketCollector(opts)
	clusterAdvanceFeatureCollector := NewClusterAdvancedFeatureCollector(opts)
	storageConsumerCollector := NewStorageConsumerCollector(opts)
	operatorConditionCollector := NewOperatorConditionCollector(opts)
	healthScoreCollector, healthScoreErr := NewHealthScoreCollector(opts)
	cephObjectStoreCollector.Run(opts.StopCh)
	cephBlockPoolCollector.Run(opts.StopCh)
	cephClusterCollector.Run(opts.StopCh)
	OBMetricsCollector.Run(opts.StopCh)
	registry.MustRegister(
		cephObjectStoreCollector,
		cephBlockPoolCollector,
		cephClusterCollector,
		OBMetricsCollector,
	)
	if clusterAdvanceFeatureCollector != nil {
		clusterAdvanceFeatureCollector.Run(opts.StopCh)
		registry.MustRegister(clusterAdvanceFeatureCollector)
	}
	if storageConsumerCollector != nil {
		storageConsumerCollector.Run(opts.StopCh)
		registry.MustRegister(storageConsumerCollector)
	}
	if storageClusterCollector != nil {
		storageClusterCollector.Run(opts.StopCh)
		registry.MustRegister(storageClusterCollector)
	}
	if storageAutoScalerCollector != nil {
		storageAutoScalerCollector.Run(opts.StopCh)
		registry.MustRegister(storageAutoScalerCollector)
	}
	if operatorConditionCollector != nil {
		operatorConditionCollector.Run(opts.StopCh)
		registry.MustRegister(operatorConditionCollector)
	}
	if healthScoreErr != nil {
		klog.Errorf("Health score collector not registered: %v", healthScoreErr)
	} else if healthScoreCollector != nil {
		healthScoreCollector.Run(opts.StopCh)
		registry.MustRegister(healthScoreCollector)
	}
}

func RegisterCephRBDCollector(registry *prometheus.Registry, conn *cephconn.Conn, opts *options.Options) {
	rbdCollector := NewCephRBDCollector(conn, opts)
	if rbdCollector != nil {
		registry.MustRegister(rbdCollector)
	}
}

// RegisterCephBlocklistCollector registers the Ceph blocklist collector to registry
func RegisterCephBlocklistCollector(registry *prometheus.Registry) {
	blocklistCollector := NewCephBlocklistCollector()
	registry.MustRegister(blocklistCollector)
}

func RegisterCephFSMetricsCollector(registry *prometheus.Registry, conn *cephconn.Conn, opts *options.Options) {
	cephFSCollector := NewCephFSSubvolumeCountCollector(conn, opts)
	if cephFSCollector != nil {
		registry.MustRegister(cephFSCollector)
	}
}
