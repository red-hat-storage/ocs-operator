package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
)

const (
	ruleName = "ocs-prometheus-rules"
)

// enablePrometheusRules is a wrapper around CreateOrUpdatePrometheusRule()
func (r *ReconcileStorageCluster) enablePrometheusRules(sc *ocsv1.StorageCluster) error {
	rule := newPrometheusRule(sc)
	err := r.CreateOrUpdatePrometheusRules(rule)
	if err != nil {
		r.reqLogger.Error(err, "unable to deploy Prometheus rules")
	}
	return nil
}

func newPrometheusRule(sc *ocsv1.StorageCluster) *monitoringv1.PrometheusRule {
	rule := &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ruleName,
			Namespace: sc.Namespace,
		},
		Spec: monitoringv1.PrometheusRuleSpec{
			Groups: []monitoringv1.RuleGroup{
				{
					Name: "cluster-service-alert.rules",
					Rules: []monitoringv1.Rule{
						{
							Alert: "ClusterObjectStoreState",
							Annotations: map[string]string{
								"description":    "Cluster Object Store is in unhealthy state for more than 15s. Please check Ceph cluster health or RGW connection.",
								"message":        "Cluster Object Store is in unhealthy state for more than 15s. Please check Ceph cluster health or RGW connection.",
								"severity_level": "error",
								"storage_type":   "RGW",
							},
							Expr: intstr.FromString("ocs_rgw_health_status{job=\"ocs-metrics-exporter\"} > 1"),
							For:  "15s",
							Labels: map[string]string{
								"severity": "critical",
							},
						},
					},
				},
			},
		},
	}
	if !sc.Spec.ExternalStorage.Enable {
		ocsRules := monitoringv1.RuleGroup{
			Name:  "ocs.rules",
			Rules: []monitoringv1.Rule{},
		}
		rule.Spec.Groups = append(rule.Spec.Groups, ocsRules)
	}
	return rule
}

// CreateOrUpdatePrometheusRules creates or updates Prometheus Rule
func (r *ReconcileStorageCluster) CreateOrUpdatePrometheusRules(rule *monitoringv1.PrometheusRule) error {
	err := r.client.Create(context.TODO(), rule)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			oldRule := &monitoringv1.PrometheusRule{}
			err = r.client.Get(context.TODO(), types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, oldRule)
			if err != nil {
				return fmt.Errorf("failed while fetching PrometheusRule: %v", err)
			}
			oldRule.Spec = rule.Spec
			err := r.client.Update(context.TODO(), oldRule)
			if err != nil {
				return fmt.Errorf("failed while updating PrometheusRule: %v", err)
			}
		} else {
			return fmt.Errorf("failed while creating PrometheusRule: %v", err)
		}
	}
	return nil
}
