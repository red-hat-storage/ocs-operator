package collectors

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/prometheus/client_golang/prometheus"
	prometheusconfig "github.com/prometheus/common/config"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/klog/v2"
)

const (
	healthScorePrometheusRuleName = "ocs-prometheus-rules"
	healthScoreRuleGroupName      = "odf_healthchecks.rules"
	alertManagerClientTimeout     = 10 * time.Second
	prometheusRuleFetchTimeout    = 10 * time.Second
	defaultAlertManagerURL        = "https://alertmanager-main.openshift-monitoring.svc.cluster.local:9094"
	serviceAccountSecretsPath     = "/var/run/secrets/kubernetes.io/serviceaccount"
	serviceAccountCACert          = serviceAccountSecretsPath + "/service-ca.crt"
	serviceAccountToken           = serviceAccountSecretsPath + "/token"
)

// Severity point deductions
const (
	infoDeduction     = 2
	warningDeduction  = 10
	criticalDeduction = 20
)

var _ prometheus.Collector = &HealthScoreCollector{}

type HealthScoreCollector struct {
	HealthScore       *prometheus.Desc
	dynClient         dynamic.Interface
	operatorNamespace string
	alertManagerURL   string
	httpClient        *http.Client
}

func NewHealthScoreCollector(opts *options.Options) (*HealthScoreCollector, error) {
	if opts.DisableHealthScore {
		klog.Info("Health score collection is disabled")
		return nil, nil
	}

	if len(opts.AllowedNamespaces) == 0 {
		return nil, fmt.Errorf("health score collector requires at least one allowed namespace")
	}

	dynClient, err := dynamic.NewForConfig(opts.Kubeconfig)
	if err != nil {
		return nil, fmt.Errorf("unable to get dynamic client for health score collector: %w", err)
	}

	httpClient, err := newAlertmanagerHTTPClient()
	if err != nil {
		return nil, fmt.Errorf("unable to build AlertManager client: %w", err)
	}

	operatorNS := opts.AllowedNamespaces[0]

	alertManagerURL := opts.AlertManagerURL
	if alertManagerURL == "" {
		alertManagerURL = defaultAlertManagerURL
	}

	return &HealthScoreCollector{
		HealthScore: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "", "health_score"),
			"ODF cluster health score (0-100) based on firing alerts",
			[]string{"namespace"},
			nil,
		),
		dynClient:         dynClient,
		operatorNamespace: operatorNS,
		alertManagerURL:   alertManagerURL,
		httpClient:        httpClient,
	}, nil
}

func (c *HealthScoreCollector) Run(stopCh <-chan struct{}) {
	// No informers to run
}

func (c *HealthScoreCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.HealthScore
}

func (c *HealthScoreCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), prometheusRuleFetchTimeout)
	defer cancel()

	alertNames := c.fetchHealthAlertNames()
	if len(alertNames) == 0 {
		return
	}

	firingAlerts, err := c.queryFiringHealthAlerts(ctx, alertNames)
	if err != nil {
		klog.Errorf("Failed to query AlertManager for health alerts: %v", err)
		return
	}

	score := c.calculateHealthScore(firingAlerts)

	ch <- prometheus.MustNewConstMetric(
		c.HealthScore,
		prometheus.GaugeValue,
		float64(score),
		c.operatorNamespace,
	)
}

func (c *HealthScoreCollector) fetchHealthAlertNames() []string {
	ctx, cancel := context.WithTimeout(context.Background(), prometheusRuleFetchTimeout)
	defer cancel()

	gvr := schema.GroupVersionResource{
		Group:    "monitoring.coreos.com",
		Version:  "v1",
		Resource: "prometheusrules",
	}

	u, err := c.dynClient.Resource(gvr).Namespace(c.operatorNamespace).Get(ctx, healthScorePrometheusRuleName, metav1.GetOptions{})
	if err != nil {
		klog.Errorf("Failed to get PrometheusRule %s: %v", healthScorePrometheusRuleName, err)
		return nil
	}

	var promRule monitoringv1.PrometheusRule
	if err := runtime.DefaultUnstructuredConverter.FromUnstructured(u.Object, &promRule); err != nil {
		klog.Errorf("Failed to decode PrometheusRule %s: %v", healthScorePrometheusRuleName, err)
		return nil
	}

	var alertNames []string
	groupFound := false
	for _, group := range promRule.Spec.Groups {
		if group.Name != healthScoreRuleGroupName {
			continue
		}
		groupFound = true
		for _, rule := range group.Rules {
			if rule.Alert != "" {
				alertNames = append(alertNames, rule.Alert)
			}
		}
	}

	if !groupFound {
		klog.Warningf("PrometheusRule %s missing expected group %s in namespace %s", healthScorePrometheusRuleName, healthScoreRuleGroupName, c.operatorNamespace)
	}

	return alertNames
}

// Alert represents an AlertManager alert
type Alert struct {
	Labels map[string]string `json:"labels"`
	Status struct {
		State       string   `json:"state"`
		SilencedBy  []string `json:"silencedBy"`
		InhibitedBy []string `json:"inhibitedBy"`
	} `json:"status"`
}

// queryFiringHealthAlerts queries AlertManager for currently firing (non-silenced) health alerts
func (c *HealthScoreCollector) queryFiringHealthAlerts(ctx context.Context, alertNames []string) ([]Alert, error) {
	if len(alertNames) == 0 {
		return nil, nil
	}

	if c.httpClient == nil {
		return nil, fmt.Errorf("alertmanager client is not initialized")
	}

	ctx, cancel := context.WithTimeout(ctx, alertManagerClientTimeout)
	defer cancel()

	url := fmt.Sprintf("%s/api/v2/alerts?active=true&silenced=false", c.alertManagerURL)

	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to query AlertManager: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("AlertManager returned status %d, failed to read error body: %w", resp.StatusCode, err)
		}
		return nil, fmt.Errorf("AlertManager returned status %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var alerts []Alert
	if err := json.Unmarshal(body, &alerts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal alerts: %w", err)
	}

	alertNameSet := make(map[string]bool)
	for _, name := range alertNames {
		alertNameSet[name] = true
	}

	var healthAlerts []Alert
	for _, alert := range alerts {
		if alertNameSet[alert.Labels["alertname"]] {
			healthAlerts = append(healthAlerts, alert)
		}
	}

	return healthAlerts, nil
}

// calculateHealthScore computes the health score from firing alerts
func (c *HealthScoreCollector) calculateHealthScore(firingAlerts []Alert) int {
	score := 100

	for _, alert := range firingAlerts {
		severity := alert.Labels["severity"]

		deduction := 0
		switch severity {
		case "info":
			deduction = infoDeduction
		case "warning":
			deduction = warningDeduction
		case "critical":
			deduction = criticalDeduction
		default:
			continue
		}

		score -= deduction
	}

	if score < 0 {
		score = 0
	}

	return score
}

func newAlertmanagerHTTPClient() (*http.Client, error) {
	tlsConfig, err := prometheusconfig.NewTLSConfig(&prometheusconfig.TLSConfig{
		CAFile: serviceAccountCACert,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create tls config: %w", err)
	}
	settings := prometheusconfig.TLSRoundTripperSettings{}
	newRT := func(cfg *tls.Config) (http.RoundTripper, error) {
		return &http.Transport{TLSClientConfig: cfg}, nil
	}
	tlsRoundTripper, err := prometheusconfig.NewTLSRoundTripperWithContext(context.Background(), tlsConfig, settings, newRT)
	if err != nil {
		return nil, fmt.Errorf("failed to create tls round tripper: %w", err)
	}
	roundTripper := prometheusconfig.NewAuthorizationCredentialsRoundTripper("Bearer", prometheusconfig.NewFileSecret(serviceAccountToken), tlsRoundTripper)

	return &http.Client{
		Transport: roundTripper,
		Timeout:   alertManagerClientTimeout,
	}, nil
}
