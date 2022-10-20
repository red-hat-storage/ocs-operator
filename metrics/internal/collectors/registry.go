package collectors

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

const (
	// name of the project/exporter
	namespace = "ocs"
)

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(registry *prometheus.Registry, opts *options.Options) {
	sharedIndexInformerList := listAllSharedIndexInformers(opts)
	for _, sii := range sharedIndexInformerList {
		go sii.Run(opts.StopCh)
	}
	var allCollectors = []prometheus.Collector{
		NewCephObjectStoreCollector(opts, sharedIndexInformerList[CephObjectStoreSIIAI]),
		NewCephBlockPoolCollector(opts, sharedIndexInformerList[CephBlockPoolSIIAI]),
		NewCephClusterCollector(opts, sharedIndexInformerList[CephClusterSIIAI]),
		NewObjectBucketCollector(opts, sharedIndexInformerList[CephObjectStoreSIIAI]),
		NewClusterAdvancedFeatureCollector(opts, sharedIndexInformerList...),
	}
	registry.MustRegister(allCollectors...)
}

var pvStoreEnabled bool
var pvStore = internalcache.NewPersistentVolumeStore()

func enablePVStore(opts *options.Options) {
	client := clientset.NewForConfigOrDie(opts.Kubeconfig)
	lw := internalcache.CreatePersistentVolumeListWatch(client, "")
	reflector := cache.NewReflector(lw, &corev1.PersistentVolume{}, pvStore, 0)
	go reflector.Run(opts.StopCh)
	pvStoreEnabled = true
}

var rbdMirrorStoreEnabled bool
var rbdMirrorStore *internalcache.RBDMirrorStore

func enableRBDMirrorStore(opts *options.Options) {
	rbdMirrorStore = internalcache.NewRBDMirrorStore(opts)
	rookClient := rookclient.NewForConfigOrDie(opts.Kubeconfig)
	lw := internalcache.CreateCephBlockPoolListWatch(rookClient, corev1.NamespaceAll, "")
	reflector := cache.NewReflector(lw, &cephv1.CephBlockPool{}, rbdMirrorStore, 30*time.Second)
	go reflector.Run(opts.StopCh)
	rbdMirrorStoreEnabled = true
}

// RegisterPersistentVolumeAttributesCollector registers PV attribute colletor to registry
func RegisterPersistentVolumeAttributesCollector(registry *prometheus.Registry, opts *options.Options) {
	if !pvStoreEnabled {
		enablePVStore(opts)
	}
	pvAttributesCollector := NewPersistentVolumeAttributesCollector(pvStore, opts)
	registry.MustRegister(pvAttributesCollector)
}

// RegisterRBDMirrorCollector registers RBD mirror metrics collector to registry
func RegisterRBDMirrorCollector(registry *prometheus.Registry, opts *options.Options) {
	if !pvStoreEnabled {
		enablePVStore(opts)
	}
	if !rbdMirrorStoreEnabled {
		enableRBDMirrorStore(opts)
	}
	rbdMirrorCollector := NewRBDMirrorCollector(rbdMirrorStore, pvStore, opts)
	registry.MustRegister(rbdMirrorCollector)
}
