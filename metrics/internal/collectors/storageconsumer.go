package collectors

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	configv1client "github.com/openshift/client-go/config/clientset/versioned/typed/config/v1"
	"github.com/prometheus/client_golang/prometheus"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/version"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &StorageConsumerCollector{}

type StorageConsumerCollector struct {
	Informer                cache.SharedIndexInformer
	StorageConsumerMetadata *prometheus.Desc
	LastHeartbeat           *prometheus.Desc
	ProviderPlatformVersion *prometheus.Desc
	ProviderOperatorVersion *prometheus.Desc
	ClientPlatformVersion   *prometheus.Desc
	ClientOperatorVersion   *prometheus.Desc
	AllowedNamespace        string
	verClient               configv1client.ClusterVersionsGetter
}

func NewStorageConsumerCollector(opts *options.Options) *StorageConsumerCollector {
	ocsClient, err := GetOcsV1alpha1Client(opts)
	if err != nil {
		klog.Error(err)
		return nil
	}
	lw := cache.NewListWatchFromClient(ocsClient, "storageconsumers", metav1.NamespaceAll, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &ocsv1alpha1.StorageConsumer{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	clientCfg, err := configv1client.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
		return nil
	}

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
		ProviderPlatformVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_provider", "platform_version"),
			`OCS StorageProvider encoded Platform Version`,
			nil, nil,
		),
		ProviderOperatorVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_provider", "operator_version"),
			`OCS StorageProvider encode Operator Version`,
			nil, nil,
		),
		ClientPlatformVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_client", "platform_version"),
			`OCS StorageClient encoded Platform Version`,
			[]string{"storage_consumer_name"},
			nil,
		),
		ClientOperatorVersion: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "storage_client", "operator_version"),
			`OCS StorageClient encoded Operator Version`,
			[]string{"storage_consumer_name"},
			nil,
		),
		Informer:  sharedIndexInformer,
		verClient: clientCfg,
	}
}

func (c *StorageConsumerCollector) Collect(ch chan<- prometheus.Metric) {
	storageConsumerLister := NewStorageConsumerLister(c.Informer.GetIndexer())
	storageConsumers := getAllStorageConsumers(storageConsumerLister, c.AllowedNamespace)
	if len(storageConsumers) > 0 {
		c.collectStorageConsumersMetadata(storageConsumers, ch)
	}
}

func (c *StorageConsumerCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.StorageConsumerMetadata,
		c.LastHeartbeat,
		c.ProviderPlatformVersion,
		c.ProviderOperatorVersion,
		c.ClientPlatformVersion,
		c.ClientOperatorVersion,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (c *StorageConsumerCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

func (c *StorageConsumerCollector) getClusterVersion() string {

	clusterVersion, err := c.verClient.ClusterVersions().Get(context.TODO(), "version", metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get clusterVersion: %v", err)
	}

	var version string
	for idx := range clusterVersion.Status.History {
		candidate := &clusterVersion.Status.History[idx]
		if candidate.State == configv1.CompletedUpdate {
			version = candidate.Version
			break
		}
	}

	if version == "" {
		klog.Errorf("Unable to find ocp version with completed update")
	}

	return version
}

// encodes version padding with 3 zeros for each part making suitable
// for numerical comparisons
// ex: 4.10.3 -> 004 010 003 -> 4010003
func encodeVersion(v string) int {
	if v == "" {
		return -1
	}

	parts := strings.Split(v, ".")
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

	ch <- prometheus.MustNewConstMetric(c.ProviderPlatformVersion,
		prometheus.GaugeValue, float64(encodeVersion(c.getClusterVersion())),
	)

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

		ch <- prometheus.MustNewConstMetric(c.ClientPlatformVersion,
			prometheus.GaugeValue,
			float64(encodeVersion(storageConsumer.Status.Client.PlatformVersion)),
			storageConsumer.Name)

		ch <- prometheus.MustNewConstMetric(c.ClientOperatorVersion,
			prometheus.GaugeValue,
			float64(encodeVersion(storageConsumer.Status.Client.OperatorVersion)),
			storageConsumer.Name)
	}
}

func getAllStorageConsumers(lister StorageConsumerLister, namespace string) (storageConsumers []*ocsv1alpha1.StorageConsumer) {
	storageConsumers, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageConsumer. %v", err)
		return nil
	}
	return
}
