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

// getDefaultOperatorPrefixes returns the list of operator name prefixes to monitor
// Add new operator prefixes here to extend monitoring to additional operators
func getDefaultOperatorPrefixes() []string {
	return []string{
		"odf-operator",
		// Add more operator prefixes here as needed
		// Example: "ocs-operator",
	}
}

func NewOperatorConditionCollector(opts *options.Options) *OperatorConditionCollector {
	c, err := GetOperatorV2Client(opts)
	if err != nil {
		klog.Errorf("Unable to get operator client: %v", err)
		return nil
	}

	// Get the operator namespace from options
	operatorNamespace := searchInNamespace(opts)

	lw := cache.NewListWatchFromClient(c, "operatorconditions", operatorNamespace, fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &operatorv2.OperatorCondition{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	return &OperatorConditionCollector{
		UpgradeableStatus: prometheus.NewDesc(
			prometheus.BuildFQName("odf", "operator", "upgradeable_status"),
			`Operator Upgradeable Status; 0: Upgradeable (True), 1: Not Upgradeable (False), 2: Unknown`,
			[]string{"name", "namespace", "reason", "message"},
			nil,
		),
		Informer:             sharedIndexInformer,
		operatorNamespace:    operatorNamespace,
		operatorNamePrefixes: getDefaultOperatorPrefixes(),
	}
}

func (c *OperatorConditionCollector) Run(stopCh <-chan struct{}) {
	go c.Informer.Run(stopCh)
}

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

// getFilteredOperatorConditions retrieves and filters operator conditions based on configured prefixes
func (c *OperatorConditionCollector) getFilteredOperatorConditions(lister Lister[operatorv2.OperatorCondition]) []*operatorv2.OperatorCondition {
	operatorConditions, err := lister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list OperatorConditions: %v", err)
		return nil
	}

	return c.filterOperatorConditions(operatorConditions)
}

// filterOperatorConditions filters operator conditions based on configured name prefixes
func (c *OperatorConditionCollector) filterOperatorConditions(conditions []*operatorv2.OperatorCondition) []*operatorv2.OperatorCondition {
	var filtered []*operatorv2.OperatorCondition
	for _, oc := range conditions {
		if c.matchesOperatorPrefix(oc.Name) {
			filtered = append(filtered, oc)
		}
	}
	return filtered
}

// matchesOperatorPrefix checks if the given name matches any configured operator prefix
func (c *OperatorConditionCollector) matchesOperatorPrefix(name string) bool {
	for _, prefix := range c.operatorNamePrefixes {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}

func (c *OperatorConditionCollector) collectUpgradeableStatus(ch chan<- prometheus.Metric, operatorConditions []*operatorv2.OperatorCondition) {
	for _, operatorCondition := range operatorConditions {
		status := 2 // Unknown by default
		reason := "Unknown"
		message := "Upgradeable condition not found"

		// Check spec.overrides first (highest priority)
		if len(operatorCondition.Spec.Overrides) > 0 {
			if s, r, m, found := findUpgradeableCondition(operatorCondition.Spec.Overrides); found {
				status = s
				reason = r
				message = m
				// If upgradeable is overridden, skip checking spec.conditions
				ch <- prometheus.MustNewConstMetric(
					c.UpgradeableStatus,
					prometheus.GaugeValue,
					float64(status),
					operatorCondition.Name,
					operatorCondition.Namespace,
					reason,
					message,
				)
				continue
			}
		}

		// Check spec.conditions if not overridden
		if len(operatorCondition.Spec.Conditions) > 0 {
			if s, r, m, found := findUpgradeableCondition(operatorCondition.Spec.Conditions); found {
				status = s
				reason = r
				message = m
				ch <- prometheus.MustNewConstMetric(
					c.UpgradeableStatus,
					prometheus.GaugeValue,
					float64(status),
					operatorCondition.Name,
					operatorCondition.Namespace,
					reason,
					message,
				)
				continue
			}
		}

		// Emit metric with unknown status if not found
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

func findUpgradeableCondition(conditions []metav1.Condition) (status int, reason, message string, found bool) {
	for _, cond := range conditions {
		if cond.Type != operatorv2.Upgradeable {
			continue
		}

		switch cond.Status {
		case metav1.ConditionTrue:
			status = 0
		case metav1.ConditionFalse:
			status = 1
		default:
			status = 2
		}

		return status, cond.Reason, cond.Message, true
	}
	return 0, "", "", false
}
