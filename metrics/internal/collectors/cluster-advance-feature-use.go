package collectors

import (
	"fmt"
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"

	storagev1 "k8s.io/api/storage/v1"
	storagev1listers "k8s.io/client-go/listers/storage/v1"
)

type ClusterAdvanceFeatureCollector struct {
	AdvancedFeature   *prometheus.Desc
	Informer          cache.SharedIndexInformer
	AllowedNamespaces []string
	// advancedFeatureMap will have 'namespace/clusterName' as key and an integer as value.
	// Value '1' indicates the cluster is using an advanced feature or else '0'.
	advancedFeatureMap map[string]int
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
		klog.Error(err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephclusters", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephCluster{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &ClusterAdvanceFeatureCollector{
		AdvancedFeature: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, advFeatureSubSystem, "usage"),
			`Indicates whether the cluster is using any advanced features, like PV/KMS encryption or external cluster mode`,
			[]string{"ceph_cluster", "namespace"},
			nil,
		),
		Informer:           sharedIndexInformer,
		AllowedNamespaces:  opts.AllowedNamespaces,
		advancedFeatureMap: make(map[string]int),
	}
}

// Run starts CephObjectStore informer
func (c *ClusterAdvanceFeatureCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
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
	cephClusterLister := cephv1listers.NewCephClusterLister(c.Informer.GetIndexer())
	cephClusters := getAllCephClusters(cephClusterLister, c.AllowedNamespaces)
	if len(cephClusters) > 0 {
		c.mapAdvanceFeatureUseFromCephClusters(cephClusters)
	}

	cephObjectStoreLister := cephv1listers.NewCephObjectStoreLister(c.Informer.GetIndexer())
	cephObjectStores := getAllObjectStores(cephObjectStoreLister, c.AllowedNamespaces)
	if len(cephObjectStores) > 0 {
		c.mapAdvanceFeatureUseFromCephObjectStores(cephObjectStores)
	}

	storageClassLister := storagev1listers.NewStorageClassLister(c.Informer.GetIndexer())
	storageClasses := getAllStorageClasses(storageClassLister, c.AllowedNamespaces)
	if len(storageClasses) > 0 {
		c.mapAdvanceFeatureUseFromStorageClasses(storageClasses)
	}

	c.collectAdvancedFeatureUse(ch)
}

func (c *ClusterAdvanceFeatureCollector) findValidKeyInNamespace(namespace string) string {
	var validKey string
	for namespaceNameKey := range c.advancedFeatureMap {
		// namespaceNameKey is in the format: <namespace>/<clusterName>
		if strings.HasPrefix(namespaceNameKey, fmt.Sprint(namespace, "/")) {
			validKey = namespaceNameKey
			break
		}
	}
	return validKey
}

func (c *ClusterAdvanceFeatureCollector) mapAdvanceFeatureUseFromCephClusters(cephClusters []*cephv1.CephCluster) {
	if c.advancedFeatureMap == nil {
		c.advancedFeatureMap = make(map[string]int)
	}
	for _, cephCluster := range cephClusters {
		// key format: '<namespace>/<cephClusterName>' and
		key := fmt.Sprint(cephCluster.Namespace, "/", cephCluster.Name)
		// map to an int value (by default 0)
		c.advancedFeatureMap[key] = 0
		if cephCluster.Spec.External.Enable ||
			cephCluster.Spec.Security.KeyManagementService.IsEnabled() {
			// if any of the above special/advanced feature is enabled, make the map value 1
			c.advancedFeatureMap[key] = 1
		}
	}
}

func (c *ClusterAdvanceFeatureCollector) mapAdvanceFeatureUseFromCephObjectStores(cephObjectStores []*cephv1.CephObjectStore) {
	for _, cephObjectStore := range cephObjectStores {
		// first check whether a cluster is available in CephObjectStore's namespace
		validKey := c.findValidKeyInNamespace(cephObjectStore.Namespace)
		// if there is no cluster in the namespace, continue with the next
		if validKey == "" {
			klog.Errorf("no cephcluster found in namespace: %q. cannot add advanced feature to CephObjectStore: %q", cephObjectStore.Namespace, cephObjectStore.Name)
			continue
		}
		if cephObjectStore.Spec.Security.KeyManagementService.IsEnabled() {
			c.advancedFeatureMap[validKey] = 1
		}
	}
}

func (c *ClusterAdvanceFeatureCollector) mapAdvanceFeatureUseFromStorageClasses(storageClasses []*storagev1.StorageClass) {
	for _, storageClass := range storageClasses {
		// first check whether a cluster is available in CephObjectStore's namespace
		validKey := c.findValidKeyInNamespace(storageClass.Namespace)
		// if there is no cluster in the namespace, continue with the next
		if validKey == "" {
			klog.Errorf("no cephcluster found in namespace: %q. cannot add advanced feature to StorageClass: %q", storageClass.Namespace, storageClass.Name)
			continue
		}
		if storageClass.Parameters["encrypted"] == "true" {
			c.advancedFeatureMap[validKey] = 1
		}
	}
}

func (c *ClusterAdvanceFeatureCollector) collectAdvancedFeatureUse(ch chan<- prometheus.Metric) {
	if c.advancedFeatureMap == nil {
		return
	}
	for k, v := range c.advancedFeatureMap {
		namespaceAndClusterName := strings.Split(k, "/")
		if len(namespaceAndClusterName) != 2 || namespaceAndClusterName[1] == "" {
			klog.Errorf("malformated map key: %+v", k)
			continue
		}
		ch <- prometheus.MustNewConstMetric(
			c.AdvancedFeature,
			prometheus.GaugeValue, float64(v),
			namespaceAndClusterName[1], namespaceAndClusterName[0],
		)
	}
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
