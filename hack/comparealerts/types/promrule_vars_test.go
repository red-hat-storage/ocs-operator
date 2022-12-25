package types_test

import (
	"encoding/json"
	"testing"

	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var (
	yamlContent1 = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    prometheus: k8s
    role: alert-rules
  name: prometheus-ocs-rules
  namespace: openshift-storage
spec:
  groups:
  - name: mirroring-alert.rules
    rules:
    - alert: OdfMirrorDaemonStatus2
      annotations:
        description: Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.
        message: Mirror daemon is unhealthy.
        severity_level: error
        storage_type: ceph
      expr: |
        ((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 0
      for: 1m
      labels:
        severity: critical
    - alert: OdfMirrorDaemonStatus
      annotations:
        description: Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.
        message: Mirror daemon is unhealthy.
        severity_level: error
        storage_type: ceph
      expr: |
        ((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 0
      for: 1m
      labels:
        severity: critical
    - alert: OdfPoolMirroringImageHealth
      annotations:
        description: Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state for more than 1m. Mirroring might not work as expected.
        message: Mirroring image(s) (PV) in the pool {{ $labels.name }} are in Unknown state.
        severity_level: warning
        storage_type: ceph
      expr: |
        (ocs_pool_mirroring_image_health{job="ocs-metrics-exporter"}  * on (namespace) group_left() (max by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"}))) == 1
      for: 1m
      labels:
        severity: warning`
	yamlContent2 = `apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  labels:
    prometheus: k8s
    role: alert-rules
  name: prometheus-ocs-rules
  namespace: openshift-storage
spec:
  groups:
  - name: mirroring-alert.rules
    rules:
    - alert: OdfMirrorDaemonStatus2
      annotations:
        description: Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.
        message: Mirror daemon is unhealthy.
        severity_level: error
        storage_type: ceph
      expr: |
        ((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 3
      for: 1m
      labels:
        severity: critical
    - alert: OdfMirrorDaemonStatus
      annotations:
        description: Mirror daemon is in unhealthy status for more than 1m. Mirroring on this cluster is not working as expected.
        message: Mirror daemon is unhealthy.
        severity_level: error
        storage_type: ceph
      expr: |
        ((count by(namespace) (ocs_mirror_daemon_count{job="ocs-metrics-exporter"} == 0)) * on(namespace) group_left() (count by(namespace) (ocs_pool_mirroring_status{job="ocs-metrics-exporter"} == 1))) > 0
      for: 1m
      labels:
        severity: critical`
)

var (
	promRuleObj = monv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "monitoring.coreos.com/v1",
			Kind:       "PrometheusRule",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "prometheus-ceph-rules",
			Namespace: "default",
			Labels: map[string]string{
				"prometheus": "k8s",
				"role":       "alert-rules",
			},
		},
		Spec: monv1.PrometheusRuleSpec{
			Groups: []monv1.RuleGroup{
				{
					Name: "ceph-mgr-status",
					Rules: []monv1.Rule{
						{
							Alert: "CephMgrIsAbsent", Annotations: map[string]string{
								"description":    "Ceph Manager has disappeared from Prometheus target discovery.",
								"message":        "Storage metrics collector service not available anymore.",
								"severity_level": "critical",
								"storage_type":   "ceph",
							},
							Expr: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "label_replace((up{job=\"rook-ceph-mgr\"} == 0 or absent(up{job=\"rook-ceph-mgr\"})), \"namespace\", \"openshift-storage\", \"\", \"\")\n",
							},
							For:    "5m",
							Labels: map[string]string{"severity": "critical"},
						},
						{
							Alert: "CephMgrIsMissingReplicas",
							Annotations: map[string]string{
								"description":    "Ceph Manager is missing replicas.",
								"message":        "Storage metrics collector service doesn't have required no of replicas.",
								"severity_level": "warning",
								"storage_type":   "ceph",
							},
							Expr: intstr.IntOrString{
								Type:   intstr.String,
								StrVal: "sum(kube_deployment_spec_replicas{deployment=~\"rook-ceph-mgr-.*\"}) by (namespace) < 1\n",
							},
							For:    "5m",
							Labels: map[string]string{"severity": "warning"},
						},
					},
				},
			},
		},
	}

	newAlert = monv1.Rule{
		Alert:  "MyNewAlert",
		Expr:   intstr.IntOrString{Type: intstr.String, StrVal: "ABC = 10"},
		For:    "1m",
		Labels: map[string]string{"id": "new-alert-1"},
	}
)

func jsonDataFromMonPrometheusRule(t *testing.T, promRule *monv1.PrometheusRule) (retStr string) {
	if promRule == nil {
		return
	}
	jsonBytes, err := json.Marshal(*promRule)
	if err != nil {
		t.Errorf("JSON marshaling failed: %v", err)
		t.FailNow()
	}
	retStr = string(jsonBytes)
	return
}

func addAnAlertToMonPromRule(alertR monv1.Rule, promRule *monv1.PrometheusRule) {
	if promRule == nil {
		return
	}
	if len(promRule.Spec.Groups) == 0 {
		return
	}
	promRule.Spec.Groups[0].Rules = append(promRule.Spec.Groups[0].Rules, alertR)
}

func changeAnAlertExpr(alertName string, expr string, promRule *monv1.PrometheusRule) {
	if promRule == nil {
		return
	}
ruleGroupFor:
	for rgIndx, rGroup := range promRule.Spec.Groups {
		for rIndx, rule := range rGroup.Rules {
			if rule.Alert == alertName {
				// rule.Expr = intstr.IntOrString{Type: intstr.String, StrVal: expr}
				promRule.Spec.Groups[rgIndx].Rules[rIndx].Expr =
					intstr.IntOrString{Type: intstr.String, StrVal: expr}
				break ruleGroupFor
			}
		}
	}
}

func TestAddAnAlertToMonPromRule(t *testing.T) {
	var promRule = monv1.Rule{
		Alert: "MyAlert",
		Expr:  intstr.IntOrString{Type: intstr.String, StrVal: "myexpression == 100"},
		For:   "1m", Labels: map[string]string{"label1": "value1"},
	}
	promRuleCopy := promRuleObj.DeepCopy()
	if len(promRuleCopy.Spec.Groups) == 0 {
		t.Errorf("empty group")
		t.FailNow()
	}
	noOfRules := len(promRuleCopy.Spec.Groups[0].Rules)
	addAnAlertToMonPromRule(promRule, promRuleCopy)
	if noOfRules+1 != len(promRuleCopy.Spec.Groups[0].Rules) {
		t.Error("there should be an increment in the no: of rules")
		t.FailNow()
	}
}

func TestChangeAnAlertExpr(t *testing.T) {
	promRuleCopy := promRuleObj.DeepCopy()
	alertName := "CephMgrIsAbsent"
	expr := "BAC=10"
	changeAnAlertExpr(alertName, expr, promRuleCopy)
	cannotFind := true
	for _, ruleGroup := range promRuleCopy.Spec.Groups {
		for _, rule := range ruleGroup.Rules {
			if rule.Alert == alertName {
				cannotFind = false
				if rule.Expr.String() != expr {
					t.Errorf("Expressions don't match, function failed")
					t.FailNow()
				}
			}
		}
	}
	if cannotFind {
		t.Errorf("Alert: %s cannot be found in the given PrometheusRule object", alertName)
		t.FailNow()
	}
}
