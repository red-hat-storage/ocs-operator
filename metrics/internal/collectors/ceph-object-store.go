package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

const (
	// component within the project/exporter
	rgwSubsystem                 = "rgw"
	cephObjectStoreUserSubsystem = "cephobjectstoreuser"
)

var _ prometheus.Collector = &CephObjectStoreCollector{}

// CephObjectStoreCollector is a custom collector for the CephObjectStore and
// CephObjectStoreUser Custom Resources
type CephObjectStoreCollector struct {
	RGWHealthStatus   *prometheus.Desc
	UserPhase         *prometheus.Desc
	Informer          cache.SharedIndexInformer
	InformerUser      cache.SharedIndexInformer
	AllowedNamespaces []string
}

// NewCephObjectStoreCollector constructs a collector
func NewCephObjectStoreCollector(opts *options.Options) *CephObjectStoreCollector {

	sharedIndexInformer := CephObjectStoreInformer(opts)
	if sharedIndexInformer == nil {
		return nil
	}

	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
		return nil
	}
	lwUser := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephobjectstoreusers", searchInNamespace(opts), fields.Everything())
	sharedIndexInformerUser := cache.NewSharedIndexInformer(lwUser, &cephv1.CephObjectStoreUser{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &CephObjectStoreCollector{
		RGWHealthStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, rgwSubsystem, "health_status"),
			`Health Status of RGW Endpoint. 0=Connected, 1=Progressing & 2=Failure`,
			[]string{"name", "namespace", "rgw_endpoint"},
			nil,
		),
		UserPhase: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, cephObjectStoreUserSubsystem, "status_phase"),
			`CephObjectStoreUser Status Phase. 0=Empty, 1=Ready, 2=Failed, 3=Reconciling, 4=ReconcileFailed`,
			[]string{"name", "namespace"},
			nil,
		),
		Informer:          sharedIndexInformer,
		InformerUser:      sharedIndexInformerUser,
		AllowedNamespaces: opts.AllowedNamespaces,
	}
}

// Run starts the CephObjectStore and CephObjectStoreUser informers
func (c *CephObjectStoreCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
	go c.InformerUser.Run(stopCh)
}

// Describe implements prometheus.Collector interface
func (c *CephObjectStoreCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.RGWHealthStatus,
		c.UserPhase,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *CephObjectStoreCollector) Collect(ch chan<- prometheus.Metric) {
	cephObjectStoreLister := cephv1listers.NewCephObjectStoreLister(c.Informer.GetIndexer())
	cephObjectStores := getAllObjectStores(cephObjectStoreLister, c.AllowedNamespaces)

	if len(cephObjectStores) > 0 {
		c.collectObjectStoreHealth(cephObjectStores, ch)
	}

	userLister := cephv1listers.NewCephObjectStoreUserLister(c.InformerUser.GetIndexer())
	users := getAllCephObjectStoreUsers(userLister, c.AllowedNamespaces)
	if len(users) > 0 {
		c.collectCephObjectStoreUserPhase(users, ch)
	}
}

func getAllObjectStores(lister cephv1listers.CephObjectStoreLister, namespaces []string) (cephObjectStores []*cephv1.CephObjectStore) {
	var tempCephObjectStores []*cephv1.CephObjectStore
	var err error
	if len(namespaces) == 0 {
		cephObjectStores, err = lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStores. %v", err)
		}
		return
	}
	for _, namespace := range namespaces {
		tempCephObjectStores, err = lister.CephObjectStores(namespace).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStores in namespace %s. %v", namespace, err)
			continue
		}
		cephObjectStores = append(cephObjectStores, tempCephObjectStores...)
	}
	return
}

func getAllCephObjectStoreUsers(lister cephv1listers.CephObjectStoreUserLister, namespaces []string) []*cephv1.CephObjectStoreUser {
	if len(namespaces) == 0 {
		result, err := lister.List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStoreUsers: %v", err)
			return nil
		}
		return result
	}
	var result []*cephv1.CephObjectStoreUser
	for _, ns := range namespaces {
		items, err := lister.CephObjectStoreUsers(ns).List(labels.Everything())
		if err != nil {
			klog.Errorf("couldn't list CephObjectStoreUsers in namespace %s: %v", ns, err)
			continue
		}
		result = append(result, items...)
	}
	return result
}

func (c *CephObjectStoreCollector) collectObjectStoreHealth(cephObjectStores []*cephv1.CephObjectStore, ch chan<- prometheus.Metric) {
	for _, cephObjectStore := range cephObjectStores {
		switch cephObjectStore.Status.Phase {
		case cephv1.ConditionConnected:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 0,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionProgressing:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 1,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionFailure:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 2,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionConnecting:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 3,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionReady:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 4,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionDeleting:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 5,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		case cephv1.ConditionDeletionIsBlocked:
			ch <- prometheus.MustNewConstMetric(c.RGWHealthStatus,
				prometheus.GaugeValue, 6,
				cephObjectStore.Name,
				cephObjectStore.Namespace,
				cephObjectStore.Status.Info["endpoint"])
		default:
			klog.Errorf("CephObjectStore in unexpected phase. Must be %q, %q or %q",
				cephv1.ConditionConnected, cephv1.ConditionProgressing, cephv1.ConditionFailure)
		}
	}
}

func (c *CephObjectStoreCollector) collectCephObjectStoreUserPhase(users []*cephv1.CephObjectStoreUser, ch chan<- prometheus.Metric) {
	for _, user := range users {
		var current string
		if user.Status != nil {
			current = user.Status.Phase
		}
		code, ok := rookGenericStatusCode(current)
		if !ok {
			klog.Errorf("CephObjectStoreUser %q in unexpected phase %q. Must be one of %v", user.Name, current, rookGenericPhases)
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.UserPhase,
			prometheus.GaugeValue, code,
			user.Name, user.Namespace)
	}
}

func CephObjectStoreInformer(opts *options.Options) cache.SharedIndexInformer {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Error(err)
		return nil
	}

	lw := cache.NewListWatchFromClient(client.CephV1().RESTClient(), "cephobjectstores", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &cephv1.CephObjectStore{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return sharedIndexInformer
}
