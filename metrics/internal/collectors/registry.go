package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
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

// RegisterPersistentVolumeAttributesCollector registers PV attribute collector to registry
func RegisterPersistentVolumeAttributesCollector(registry *prometheus.Registry) {
	pvAttributesCollector := NewPersistentVolumeAttributesCollector()
	registry.MustRegister(pvAttributesCollector)
}

// RegisterRBDMirrorCollector registers RBD mirror metrics collector to registry
func RegisterRBDMirrorCollector(registry *prometheus.Registry) {
	rbdMirrorCollector := NewRBDMirrorCollector()
	registry.MustRegister(rbdMirrorCollector)
}

func RegisterCephBlocklistCollector(registry *prometheus.Registry) {
	blocklistCollector := NewCephBlocklistCollector()
	registry.MustRegister(blocklistCollector)
}

func RegisterCephRBDChildrenCollector(registry *prometheus.Registry) {
	childrenCollector := NewCephRBDChildrenCollector()
	registry.MustRegister(childrenCollector)
}

func RegisterCephFSMetricsCollector(registry *prometheus.Registry) {
	cephFSMetricsCollector := NewCephFSSubvolumeCountCollector()
	registry.MustRegister(cephFSMetricsCollector)
}
