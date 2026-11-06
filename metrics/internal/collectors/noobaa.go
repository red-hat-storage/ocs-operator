package collectors

import (
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

// GetNooBaaV1alpha1Client builds a REST client scoped to the NooBaa CRD group/version,
// since noobaa-operator doesn't ship a generated clientset.
func GetNooBaaV1alpha1Client(opts *options.Options) (*rest.RESTClient, error) {
	config, err := clientcmd.BuildConfigFromFlags(opts.Apiserver, opts.KubeconfigPath)
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(nbv1.SchemeBuilder.AddToScheme(scheme))
	codecs := serializer.NewCodecFactory(scheme)
	config.GroupVersion = &nbv1.SchemeGroupVersion
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	if config.UserAgent == "" {
		config.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	client, err := rest.RESTClientFor(config)
	if err != nil {
		return nil, err
	}
	return client, nil
}

type noobaaLister struct {
	indexer cache.Indexer
}

// NewNooBaaLister returns a Lister backed by the given indexer.
func NewNooBaaLister(indexer cache.Indexer) Lister[nbv1.NooBaa] {
	return &noobaaLister{indexer: indexer}
}

func (n *noobaaLister) List(selector labels.Selector) (ret []*nbv1.NooBaa, err error) {
	err = cache.ListAll(n.indexer, selector, func(m interface{}) {
		ret = append(ret, m.(*nbv1.NooBaa))
	})
	return ret, err
}

const noobaaSubsystem = "noobaa"

// noobaaPhases enumerates every nbv1.SystemPhase value a NooBaa's status.phase can
// report, in the same order as the numeric codes returned by noobaaStatusCode.
var noobaaPhases = []nbv1.SystemPhase{
	nbv1.SystemPhaseRejected,
	nbv1.SystemPhaseVerifying,
	nbv1.SystemPhaseCreating,
	nbv1.SystemPhaseConnecting,
	nbv1.SystemPhaseConfiguring,
	nbv1.SystemPhaseReady,
}

// noobaaStatusCode maps a NooBaa status.phase to its numeric status_phase gauge
// value: 0=Rejected, 1=Verifying, 2=Creating, 3=Connecting, 4=Configuring, 5=Ready.
// ok is false if phase doesn't match any known value, in which case the caller
// should skip emitting the metric (and log the unexpected value itself, since it
// has the resource name/namespace for context).
func noobaaStatusCode(phase nbv1.SystemPhase) (code float64, ok bool) {
	switch phase {
	case nbv1.SystemPhaseRejected:
		return 0, true
	case nbv1.SystemPhaseVerifying:
		return 1, true
	case nbv1.SystemPhaseCreating:
		return 2, true
	case nbv1.SystemPhaseConnecting:
		return 3, true
	case nbv1.SystemPhaseConfiguring:
		return 4, true
	case nbv1.SystemPhaseReady:
		return 5, true
	default:
		return 0, false
	}
}

var _ prometheus.Collector = &NooBaaCollector{}

// NooBaaCollector is a custom collector for the NooBaa Custom Resource
type NooBaaCollector struct {
	Phase    *prometheus.Desc
	Informer cache.SharedIndexInformer
}

// NewNooBaaCollector constructs a collector
func NewNooBaaCollector(opts *options.Options) *NooBaaCollector {
	client, err := GetNooBaaV1alpha1Client(opts)
	if err != nil {
		klog.Errorf("failed to create client for NooBaa collector: %v", err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client, "noobaas", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &nbv1.NooBaa{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &NooBaaCollector{
		Phase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, noobaaSubsystem, "status_phase"),
			`NooBaa Status Phase. 0=Rejected, 1=Verifying, 2=Creating, 3=Connecting, 4=Configuring, 5=Ready`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}

// Run starts the NooBaa informer
func (c *NooBaaCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *NooBaaCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.Phase
}

// Collect implements prometheus.Collector interface
func (c *NooBaaCollector) Collect(ch chan<- prometheus.Metric) {
	lister := NewNooBaaLister(c.Informer.GetIndexer())
	noobaas, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list NooBaa: %v", err)
		return
	}
	if len(noobaas) > 0 {
		c.collectNooBaaPhase(noobaas, ch)
	}
}

func (c *NooBaaCollector) collectNooBaaPhase(noobaas []*nbv1.NooBaa, ch chan<- prometheus.Metric) {
	for _, nb := range noobaas {
		code, ok := noobaaStatusCode(nb.Status.Phase)
		if !ok {
			klog.Errorf("NooBaa %q in unexpected phase %q. Must be one of %v", nb.Name, nb.Status.Phase, noobaaPhases)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.Phase,
			prometheus.GaugeValue, code,
			nb.Name, nb.Namespace)
	}
}
