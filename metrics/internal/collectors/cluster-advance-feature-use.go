package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	storagev1 "k8s.io/api/storage/v1"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
)

type AdvancedFeatureProvider interface {
	cache.SharedIndexInformer
	AdvancedFeature(namespaces ...string) int
}

type CephClusterAdvancedFeatureProvider struct {
	cache.SharedIndexInformer
}

func (c *CephClusterAdvancedFeatureProvider) AdvancedFeature(namespaces ...string) int {
	allCephClusters := getAllCephClusters(
		cephv1listers.NewCephClusterLister(c.GetIndexer()), namespaces)
	for _, cephCluster := range allCephClusters {
		if cephCluster.Spec.External.Enable {
			return 1
		} else if cephCluster.Spec.Security.KeyManagementService.IsEnabled() {
			return 1
		}
	}
	return 0
}

func NewCephClusterAdvancedFeatureProvider(client *rookclient.Clientset) AdvancedFeatureProvider {
	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephclusters", metav1.NamespaceAll, fields.Everything())
	return &CephClusterAdvancedFeatureProvider{
		SharedIndexInformer: cache.NewSharedIndexInformer(lw, &cephv1.CephCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}),
	}
}

type CephObjectStoreAdvancedFeatureProvider struct {
	cache.SharedIndexInformer
}

func (c *CephObjectStoreAdvancedFeatureProvider) AdvancedFeature(namespaces ...string) int {
	cephObjectStoreLister := cephv1listers.NewCephObjectStoreLister(c.GetIndexer())
	cephObjectStores := getAllObjectStores(cephObjectStoreLister, namespaces)
	for _, cephObjectStore := range cephObjectStores {
		if cephObjectStore.Spec.Security == nil {
			return 0
		}
		if cephObjectStore.Spec.Security.KeyManagementService.IsEnabled() {
			return 1
		}
	}
	return 0
}

func NewCephObjectStoreAdvancedFeatureProvider(client *rookclient.Clientset) AdvancedFeatureProvider {
	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephobjectstores", metav1.NamespaceAll, fields.Everything())
	return &CephObjectStoreAdvancedFeatureProvider{
		SharedIndexInformer: cache.NewSharedIndexInformer(lw, &cephv1.CephObjectStore{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}),
	}
}

type StorageClassAdvancedFeatureProvider struct {
	cache.SharedIndexInformer
}

func (s *StorageClassAdvancedFeatureProvider) AdvancedFeature(namespaces ...string) int {
	storageClassLister := storagev1listers.NewStorageClassLister(s.GetIndexer())
	storageClasses := getAllStorageClasses(storageClassLister, namespaces)
	for _, storageClass := range storageClasses {
		if storageClass.Parameters["encrypted"] == "true" {
			return 1
		}
	}
	return 0
}

func NewStorageClassAdvancedFeatureProvider(client *kubernetes.Clientset) AdvancedFeatureProvider {
	storageclassClient := client.StorageV1()
	lw := cache.NewListWatchFromClient(storageclassClient.RESTClient(), "storageclasses", metav1.NamespaceAll, fields.Everything())
	return &StorageClassAdvancedFeatureProvider{
		SharedIndexInformer: cache.NewSharedIndexInformer(lw, &storagev1.StorageClass{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}),
	}
}

type CephRBDMirrorAdvancedFeatureProvider struct {
	cache.SharedIndexInformer
}

func (c *CephRBDMirrorAdvancedFeatureProvider) AdvancedFeature(namespaces ...string) int {
	cephRBDMirrorLister := cephv1listers.NewCephRBDMirrorLister(c.GetIndexer())
	cephRBDMirrors := getAllRBDMirrors(cephRBDMirrorLister, namespaces)
	for _, rbdM := range cephRBDMirrors {
		if rbdM.Spec.Count > 0 {
			return 1
		}
	}
	return 0
}

func NewCephRBDMirrorAdvancedFeatureProvider(client *rookclient.Clientset) AdvancedFeatureProvider {
	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephrbdmirrors", metav1.NamespaceAll, fields.Everything())
	return &CephRBDMirrorAdvancedFeatureProvider{
		SharedIndexInformer: cache.NewSharedIndexInformer(lw, &cephv1.CephRBDMirror{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}),
	}
}

type ClusterAdvanceFeatureCollector struct {
	AdvancedFeature     *prometheus.Desc
	AllowedNamespaces   []string
	advFeatureProviders []AdvancedFeatureProvider
}

const (
	// component within the project/exporter
	advFeatureSubSystem = "advanced_feature"
)

var _ prometheus.Collector = &ClusterAdvanceFeatureCollector{}

// NewClusterAdvancedFeatureCollector constructs the StorageCluster's advanced-feature collector
func NewClusterAdvancedFeatureCollector(opts *options.Options) *ClusterAdvanceFeatureCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("cluster advanced feature collector failed to create client: %v", err)
		return nil
	}

	advFeatureProviders := []AdvancedFeatureProvider{
		NewCephClusterAdvancedFeatureProvider(client),
		NewCephObjectStoreAdvancedFeatureProvider(client),
		NewCephRBDMirrorAdvancedFeatureProvider(client),
	}

	if k8Client, err := kubernetes.NewForConfig(opts.Kubeconfig); err == nil {
		advFeatureProviders = append(
			advFeatureProviders, NewStorageClassAdvancedFeatureProvider(k8Client))
	} else { // logging any error occurred
		klog.Errorf("unable to get K8 Client, no StorageClass information available: %v", err)
	}

	return &ClusterAdvanceFeatureCollector{
		AdvancedFeature: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, advFeatureSubSystem, "usage"),
			`Indicates whether the cluster is using any advanced features, like PV/KMS encryption or external cluster mode`,
			nil, nil,
		),
		AllowedNamespaces:   opts.AllowedNamespaces,
		advFeatureProviders: advFeatureProviders,
	}
}

// Run starts all the SharedIndex informers
func (c *ClusterAdvanceFeatureCollector) Run(stopCh <-chan struct{}) {
	for _, informer := range c.advFeatureProviders {
		go informer.Run(stopCh)
	}
}

// Describe implements prometheus.Collector interface
func (c *ClusterAdvanceFeatureCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.AdvancedFeature,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *ClusterAdvanceFeatureCollector) Collect(ch chan<- prometheus.Metric) {
	for _, advProvider := range c.advFeatureProviders {
		// if any of the provider gives an advanced feature,
		// collect it and return immediately
		if advFeature := advProvider.AdvancedFeature(c.AllowedNamespaces...); advFeature > 0 {
			c.collectAdvancedFeatureUse(ch, advFeature)
			return
		}
	}
	c.collectAdvancedFeatureUse(ch, 0)
}

func (c *ClusterAdvanceFeatureCollector) collectAdvancedFeatureUse(ch chan<- prometheus.Metric, advancedFeature int) {
	ch <- prometheus.MustNewConstMetric(
		c.AdvancedFeature,
		prometheus.GaugeValue, float64(advancedFeature),
	)
}

func getAllStorageClasses(
	lister storagev1listers.StorageClassLister,
	namespaces []string) []*storagev1.StorageClass {
	var err error
	allSCs, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageClasses. %v", err)
		return nil
	}
	if len(namespaces) == 0 {
		return allSCs
	}
	var namespacedSCs []*storagev1.StorageClass
	for _, namespace := range namespaces {
		for _, eachSC := range allSCs {
			if eachSC.Namespace == namespace {
				namespacedSCs = append(namespacedSCs, eachSC)
			}
		}
	}
	return namespacedSCs
}

func getAllRBDMirrors(lister cephv1listers.CephRBDMirrorLister, namespaces []string) []*cephv1.CephRBDMirror {
	var err error
	allRBDMirrors, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list RBD Mirrors. %v", err)
		return nil
	}
	if len(namespaces) == 0 {
		return allRBDMirrors
	}
	var namespacedRBDMirrors []*cephv1.CephRBDMirror
	for _, namespace := range namespaces {
		for _, eachRBDMirror := range allRBDMirrors {
			if eachRBDMirror.Namespace == namespace {
				namespacedRBDMirrors = append(namespacedRBDMirrors, eachRBDMirror)
			}
		}
	}
	return namespacedRBDMirrors
}
