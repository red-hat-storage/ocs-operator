package testunit

import (
	"github.com/prometheus/common/model"
)

// for more details refer:
// https://github.com/prometheus/prometheus/blob/main/cmd/promtool/unittest.go

type PrometheusTestUnit struct {
	EvaluationInterval model.Duration `json:"evaluation_interval,omitempty" yaml:"evaluation_interval,omitempty"`
	RuleFiles          []string       `json:"rule_files" yaml:"rule_files"`
	GroupEvalOrder     []string       `json:"group_eval_order,omitempty" yaml:"group_eval_order,omitempty"`
	Tests              []TestGroup    `json:"tests,omitempty" yaml:"tests,omitempty"`
}

type TestGroup struct {
	Interval        model.Duration    `json:"interval" yaml:"interval"`
	InputSeries     []Series          `json:"input_series" yaml:"input_series"`
	TestGroupName   string            `json:"name,omitempty" yaml:"name,omitempty"`
	AlertRuleTests  []AlertTestCase   `json:"alert_rule_test,omitempty" yaml:"alert_rule_test,omitempty"`
	PromQLExprTests []PromQLTestCase  `json:"promql_expr_test,omitempty" yaml:"promql_expr_test,omitempty"`
	ExternalLabels  map[string]string `json:"external_labels,omitempty" yaml:"external_labels,omitempty"`
	ExternalURL     string            `json:"external_url,omitempty" yaml:"external_url,omitempty"`
}

type Series struct {
	Series string `json:"series" yaml:"series"`
	Values string `json:"values,omitempty" yaml:"values,omitempty"`
}

type AlertTestCase struct {
	EvalTime       model.Duration `json:"eval_time" yaml:"eval_time"`
	AlertName      string         `json:"alertname" yaml:"alertname"`
	ExpectedAlerts []Alert        `json:"exp_alerts,omitempty" yaml:"exp_alerts,omitempty"`
}

type Alert struct {
	ExpectedLabels      map[string]string `json:"exp_labels,omitempty" yaml:"exp_labels,omitempty"`
	ExpectedAnnotations map[string]string `json:"exp_annotations,omitempty" yaml:"exp_annotations,omitempty"`
}

type PromQLTestCase struct {
	Expr            string         `json:"expr" yaml:"expr"`
	EvalTime        model.Duration `json:"eval_time" yaml:"eval_time"`
	ExpectedSamples []Sample       `json:"exp_samples,omitempty" yaml:"exp_samples,omitempty"`
}

type Sample struct {
	Labels    string  `json:"labels" yaml:"labels"`
	Value     float64 `json:"value" yaml:"value"`
	Histogram string  `json:"histogram,omitempty" yaml:"histogram,omitempty"`
	// A non-empty 'Histogram' string means 'Value' is ignored.
}
