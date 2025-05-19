package collectors

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/blang/semver/v4"
	"github.com/prometheus/client_golang/prometheus"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/version"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &StorageConsumerCollector{}

type StorageConsumerCollector struct {
	Informer                     cache.SharedIndexInformer
	StorageConsumerMetadata      *prometheus.Desc
	LastHeartbeat                *prometheus.Desc
	StorageQuotaUtilizationRatio *prometheus.Desc
	ProviderOperatorVersion      *prometheus.Desc
	ClientOperatorVersion        *prometheus.Desc
	AllowedNamespace             string
}

func NewStorageConsumerCollector(opts *options.Options) *StorageConsumerCollector {
	ocsClient, err := GetOcsV1alpha1Client(opts)
	if err != nil {
		klog.Error(err)
		return nil
	}
	lw := cache.NewListWatchFromClient(ocsClient, "storageconsumers", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &ocsv1alpha1.StorageConsumer{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &StorageConsumerCollector{
		StorageConsumerMetadata: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_consumer", "metadata"),
			`Attributes of OCS Storage Consumers`,
			[]string{"storage_consumer_name", "state"},
			nil,
		),
		LastHeartbeat: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_client", "last_heartbeat"),
			`Unixtime (in sec) of last heartbeat of OCS Storage Client`,
			[]string{"storage_consumer_name"},
			nil,
		),
		ProviderOperatorVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_provider", "operator_version"),
			`OCS StorageProvider encode Operator Version`,
			nil, nil,
		),
		ClientOperatorVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_client", "operator_version"),
			`OCS StorageClient encoded Operator Version`,
			[]string{"storage_consumer_name"},
			nil,
		),
		StorageQuotaUtilizationRatio: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_client", "storage_quota_utilization_ratio"),
			`StorageQuotaUtilizationRatio of ODF Storage Client`,
			[]string{"client_name", "client_cluster_name"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

func (c *StorageConsumerCollector) Collect(ch chan<- prometheus.Metric) {
	storageConsumerLister := NewStorageConsumerLister(c.Informer.GetIndexer())
	storageConsumers := getAllStorageConsumers(storageConsumerLister)
	if len(storageConsumers) > 0 {
		c.collectStorageConsumersMetadata(storageConsumers, ch)
	}
}

func (c *StorageConsumerCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.StorageConsumerMetadata,
		c.LastHeartbeat,
		c.ProviderOperatorVersion,
		c.ClientOperatorVersion,
		c.StorageQuotaUtilizationRatio,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (c *StorageConsumerCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// encodes version padding with 3 zeros for each part making suitable
// for numerical comparisons
// ex: 4.10.3 -> 004 010 003 -> 4010003
func encodeVersion(version string) int {

	fv, err := semver.FinalizeVersion(version)
	if err != nil {
		klog.Warningf("Failed to parse %q as semver version: %v", version, err)
		return -1
	}

	parts := strings.Split(fv, ".")
	if len(parts) != 3 {
		return -1
	}
	sb := make([]string, 3)
	for i := range sb {
		sb[i] = fmt.Sprintf("%03s", parts[i])
	}

	ver := strings.Join(sb, "")
	encode, err := strconv.Atoi(ver)
	if err != nil {
		return -1
	}

	return encode
}

func (c *StorageConsumerCollector) collectStorageConsumersMetadata(storageConsumers []*ocsv1alpha1.StorageConsumer, ch chan<- prometheus.Metric) {

	ch <- prometheus.MustNewConstMetric(c.ProviderOperatorVersion,
		prometheus.GaugeValue, float64(encodeVersion(version.GetVersion())),
	)

	for _, storageConsumer := range storageConsumers {
		ch <- prometheus.MustNewConstMetric(c.StorageConsumerMetadata,
			prometheus.GaugeValue, 1,
			storageConsumer.Name,
			string(storageConsumer.Status.State))

		ch <- prometheus.MustNewConstMetric(c.LastHeartbeat,
			prometheus.GaugeValue, float64(storageConsumer.Status.LastHeartbeat.Time.Unix()),
			storageConsumer.Name)

		if storageConsumer.Status.Client != nil {
			ch <- prometheus.MustNewConstMetric(c.ClientOperatorVersion,
				prometheus.GaugeValue,
				float64(encodeVersion(storageConsumer.Status.Client.OperatorVersion)),
				storageConsumer.Name)

			ch <- prometheus.MustNewConstMetric(c.StorageQuotaUtilizationRatio,
				prometheus.GaugeValue, storageConsumer.Status.Client.StorageQuotaUtilizationRatio,
				storageConsumer.Status.Client.Name, storageConsumer.Status.Client.ClusterName)
		}
	}
}

func getAllStorageConsumers(lister StorageConsumerLister) (storageConsumers []*ocsv1alpha1.StorageConsumer) {
	storageConsumers, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageConsumer. %v", err)
		return nil
	}
	return
}
