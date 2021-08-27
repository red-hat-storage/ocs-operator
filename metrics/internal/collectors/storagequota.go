package collectors

import (
	"context"
	"strings"

	quotav1 "github.com/openshift/api/quota/v1"
	quotaclient "github.com/openshift/client-go/quota/clientset/versioned"
	quotav1lister "github.com/openshift/client-go/quota/listers/quota/v1"
	"github.com/openshift/ocs-operator/controllers/storagecluster"
	"github.com/openshift/ocs-operator/metrics/internal/options"
	"github.com/prometheus/client_golang/prometheus"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var _ prometheus.Collector = &StorageQuotaCollector{}

// StorageQuotaCollector is a custom collector for ClusterResourceQuota/storage resources
type StorageQuotaCollector struct {
	HardDesc *prometheus.Desc
	UsedDesc *prometheus.Desc
	Informer cache.SharedIndexInformer
}

// storageQuotaInfo is a simplified metric entry per each ClusterResourceQuota storage entry
type storageQuotaInfo struct {
	Name             string
	StorageClassName string
	Quantity         resource.Quantity
}

// NewStorageQuotaCollector constructs a collector
func NewStorageQuotaCollector(opts *options.Options) *StorageQuotaCollector {
	quotaClient := quotaclient.NewForConfigOrDie(opts.Kubeconfig)
	lw := newListWatchFromQuotaClient(quotaClient, fields.Everything())
	indexers := cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &quotav1.ClusterResourceQuota{}, 0, indexers)
	return &StorageQuotaCollector{
		HardDesc: prometheus.NewDesc(
			"ocs_clusterresourcequota_hard",
			`OCS ClusterResourceQuota/storage hard-limit`,
			[]string{"name", "storageclass"},
			nil,
		),
		UsedDesc: prometheus.NewDesc(
			"ocs_clusterresourcequota_used",
			`OCS ClusterResourceQuota/storage currently-used`,
			[]string{"name", "storageclass"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

// Run starts CephObjectStore informer
func (c *StorageQuotaCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *StorageQuotaCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.HardDesc,
		c.UsedDesc,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *StorageQuotaCollector) Collect(ch chan<- prometheus.Metric) {
	storageQuotaHard, storageQuotaUsed := c.listStorageQuotas()
	for _, storageQuota := range storageQuotaHard {
		ch <- prometheus.MustNewConstMetric(c.HardDesc,
			prometheus.GaugeValue,
			storageQuota.Quantity.AsApproximateFloat64(),
			storageQuota.Name,
			storageQuota.StorageClassName)
	}
	for _, storageQuota := range storageQuotaUsed {
		ch <- prometheus.MustNewConstMetric(c.UsedDesc,
			prometheus.GaugeValue,
			storageQuota.Quantity.AsApproximateFloat64(),
			storageQuota.Name,
			storageQuota.StorageClassName)
	}
}

func (c *StorageQuotaCollector) listStorageQuotas() ([]storageQuotaInfo, []storageQuotaInfo) {
	storageQuotaInfoHard := []storageQuotaInfo{}
	storageQuotaInfoUsed := []storageQuotaInfo{}
	clusterResourceQuotas, err := quotav1lister.NewClusterResourceQuotaLister(c.Informer.GetIndexer()).List(labels.Everything())
	if err != nil {
		klog.Errorf("failed to list ClusterResourceQuota: %v", err)
		return storageQuotaInfoHard, storageQuotaInfoUsed
	}
	for _, clusterResourceQuota := range clusterResourceQuotas {
		for resource := range clusterResourceQuota.Status.Total.Hard {
			if isStorageResource(resource) {
				storageQuotaHard := storageQuotaInfo{
					Name:             clusterResourceQuota.Name,
					StorageClassName: storagecluster.StorageClassByV1Resource(resource),
					Quantity:         clusterResourceQuota.Status.Total.Hard[resource],
				}
				storageQuotaInfoHard = append(storageQuotaInfoHard, storageQuotaHard)
				break
			}
		}
		for resource := range clusterResourceQuota.Status.Total.Used {
			if isStorageResource(resource) {
				storageQuotaUsed := storageQuotaInfo{
					Name:             clusterResourceQuota.Name,
					StorageClassName: storagecluster.StorageClassByV1Resource(resource),
					Quantity:         clusterResourceQuota.Status.Total.Used[resource],
				}
				storageQuotaInfoUsed = append(storageQuotaInfoUsed, storageQuotaUsed)
				break
			}
		}
	}
	return storageQuotaInfoHard, storageQuotaInfoUsed
}

func isStorageResource(resource corev1.ResourceName) bool {
	return strings.Contains(string(resource), string(corev1.ResourceStorage))
}

func newListWatchFromQuotaClient(quotaClient *quotaclient.Clientset, fieldSelector fields.Selector) *cache.ListWatch {
	return &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			return quotaClient.QuotaV1().ClusterResourceQuotas().List(context.TODO(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.Watch = true
			return quotaClient.QuotaV1().ClusterResourceQuotas().Watch(context.TODO(), options)
		},
	}
}
