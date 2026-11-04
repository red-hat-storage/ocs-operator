package collectors

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog/v2"
)

const (
	// odfOperatorInternalConfigMapName is the ConfigMap odf-operator uses to
	// persist the last accepted CSV build date and whether dependent operator
	// subscription channel upgrades are currently blocked by the build-date
	// validation check (see odf-operator controllers/subscription_controller.go).
	odfOperatorInternalConfigMapName = "odf-operator-internal-config"

	lastAcceptedCSVKey            = "LAST_ACCEPTED_CSV"
	lastAcceptedCSVBuildDateKey   = "LAST_ACCEPTED_CSV_BUILD_DATE"
	dependenciesUpgradeBlockedKey = "DEPENDENCIES_UPGRADE_BLOCKED"
)

var _ prometheus.Collector = &OdfUpgradeStatusCollector{}

// OdfUpgradeStatusCollector exposes the dependencies-upgrade-blocked state
// that odf-operator records in its internal ConfigMap when the target
// build's creation date is older than the allowed threshold compared to the
// last accepted build.
type OdfUpgradeStatusCollector struct {
	DependenciesUpgradeBlocked *prometheus.Desc
	k8sclient                  kubernetes.Interface
	operatorNamespace          string
}

func NewOdfUpgradeStatusCollector(opts *options.Options) *OdfUpgradeStatusCollector {
	if len(opts.AllowedNamespaces) == 0 {
		klog.Error("Odf upgrade status collector not registered: no allowed namespaces")
		return nil
	}

	return &OdfUpgradeStatusCollector{
		DependenciesUpgradeBlocked: prometheus.NewDesc(
			prometheus.BuildFQName("odf", "operator", "dependencies_upgrade_blocked"),
			`Whether odf-operator has paused dependent operator subscription upgrades because the target build is older than the allowed threshold compared to the last accepted build; 0: not blocked, 1: blocked`,
			[]string{"namespace", "last_accepted_csv", "last_accepted_csv_build_date"},
			nil,
		),
		k8sclient:         kubernetes.NewForConfigOrDie(opts.Kubeconfig),
		operatorNamespace: opts.AllowedNamespaces[0],
	}
}

func (c *OdfUpgradeStatusCollector) Run(_ <-chan struct{}) {}

func (c *OdfUpgradeStatusCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.DependenciesUpgradeBlocked
}

func (c *OdfUpgradeStatusCollector) Collect(ch chan<- prometheus.Metric) {
	cm, err := c.k8sclient.CoreV1().ConfigMaps(c.operatorNamespace).Get(context.Background(), odfOperatorInternalConfigMapName, metav1.GetOptions{})
	if err != nil {
		if !apierrors.IsNotFound(err) {
			klog.Errorf("couldn't get configmap %q: %v", odfOperatorInternalConfigMapName, err)
		}
		return
	}

	blocked := 0.0
	if cm.Data[dependenciesUpgradeBlockedKey] == "true" {
		blocked = 1.0
	}

	ch <- prometheus.MustNewConstMetric(
		c.DependenciesUpgradeBlocked,
		prometheus.GaugeValue,
		blocked,
		cm.Namespace,
		cm.Data[lastAcceptedCSVKey],
		cm.Data[lastAcceptedCSVBuildDateKey],
	)
}
