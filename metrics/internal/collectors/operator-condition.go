package collectors

import (
	"strings"

	operatorv2 "github.com/operator-framework/api/pkg/operators/v2"
	"github.com/prometheus/client_golang/prometheus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"

	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
)

type OperatorConditionCollector struct {
	UpgradeableStatus    *prometheus.Desc
	Informer             cache.SharedIndexInformer
	operatorNamespace    string
	operatorNamePrefixes []string
}

var _ prometheus.Collector = &OperatorConditionCollector{}

func (c *OperatorConditionCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.UpgradeableStatus,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (c *OperatorConditionCollector) Collect(ch chan<- prometheus.Metric) {
	ocLister := NewOperatorConditionLister(c.Informer.GetIndexer())
	operatorConditions := c.getFilteredOperatorConditions(ocLister)
	if len(operatorConditions) == 0 {
		return
	}
	c.collectUpgradeableStatus(ch, operatorConditions)
}

func (c *OperatorConditionCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

func getOperatorNamePrefixes() []string {
	return []string{
		"odf-operator",
	}
}

func NewOperatorConditionCollector(opts *options.Options) *OperatorConditionCollector {
	c, err := GetOperatorV2Client(opts)
	if err != nil {
		klog.Errorf("Unable to get operator client: %v", err)
		return nil
	}

	operatorNamespace := searchInNamespace(opts)
	lw := cache.NewListWatchFromClient(c, "operatorconditions", operatorNamespace, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &operatorv2.OperatorCondition{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &OperatorConditionCollector{
		UpgradeableStatus: prometheus.NewDesc(
			prometheus.BuildFQName("odf", "operator", "upgradeable_status"),
			`Operator Upgradeable Status; 0: Not Upgradeable (False), 1: Upgradeable (True)`,
			[]string{"name", "namespace", "reason", "message"},
			nil,
		),
		Informer:             sharedIndexInformer,
		operatorNamespace:    operatorNamespace,
		operatorNamePrefixes: getOperatorNamePrefixes(),
	}
}

// getFilteredOperatorConditions retrieves and filters operator conditions based on configured prefixes
func (c *OperatorConditionCollector) getFilteredOperatorConditions(lister Lister[operatorv2.OperatorCondition]) []*operatorv2.OperatorCondition {
	operatorConditions, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list OperatorConditions: %v", err)
		return nil
	}

	var filtered []*operatorv2.OperatorCondition
	for _, oc := range operatorConditions {
		for _, prefix := range c.operatorNamePrefixes {
			if strings.HasPrefix(oc.Name, prefix) {
				filtered = append(filtered, oc)
				break
			}
		}
	}
	return filtered
}

func (c *OperatorConditionCollector) collectUpgradeableStatus(ch chan<- prometheus.Metric, operatorConditions []*operatorv2.OperatorCondition) {
	for _, operatorCondition := range operatorConditions {
		status, reason, message := c.getUpgradeableStatus(operatorCondition)
		ch <- prometheus.MustNewConstMetric(
			c.UpgradeableStatus,
			prometheus.GaugeValue,
			float64(status),
			operatorCondition.Name,
			operatorCondition.Namespace,
			reason,
			message,
		)
	}
}

func (c *OperatorConditionCollector) getUpgradeableStatus(operatorCondition *operatorv2.OperatorCondition) (int, string, string) {
	// Check spec.overrides first (highest priority)
	if s, r, m, found := findUpgradeableCondition(operatorCondition.Spec.Overrides); found {
		return s, r, m
	}

	// if not found in 'overrides', check spec.conditions next
	if s, r, m, found := findUpgradeableCondition(operatorCondition.Spec.Conditions); found {
		return s, r, m
	}

	// Upgradeable (that is status == 1), by default (aligning with default OLM behaviour)
	return 1, "AssumedUpgradeable", "No upgradeable condition found, assuming upgradeable"
}

func findUpgradeableCondition(conditions []metav1.Condition) (status int, reason, message string, found bool) {
	for _, cond := range conditions {
		if cond.Type != operatorv2.Upgradeable {
			continue
		}

		switch cond.Status {
		case metav1.ConditionTrue:
			status = 1 // Upgradeable
		case metav1.ConditionFalse:
			status = 0 // Not Upgradeable
		default:
			status = 1 // Treat unknown status as upgradeable (OLM behavior)
		}

		return status, cond.Reason, cond.Message, true
	}
	return 0, "", "", false
}
