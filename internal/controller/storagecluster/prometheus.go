package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"dario.cat/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	internalPrometheusRuleFilepath = "/ocs-prometheus-rules/prometheus-ocs-rules.yaml"
	externalPrometheusRuleFilepath = "/ocs-prometheus-rules/prometheus-ocs-rules-external.yaml"
	ruleName                       = "ocs-prometheus-rules"
)

// enablePrometheusRules is a wrapper around CreateOrUpdatePrometheusRule()
func (r *StorageClusterReconciler) enablePrometheusRules(ctx context.Context, instance *ocsv1.StorageCluster) error {
	var excludedAlertNames = make([]string, 0, len(instance.Spec.Monitoring.ExcludedAlerts))
	if instance.Spec.Monitoring != nil {
		for _, excludedAlert := range instance.Spec.Monitoring.ExcludedAlerts {
			excludedAlertNames = append(excludedAlertNames, excludedAlert.AlertName)
		}
	}

	rule, err := getPrometheusRules(instance.Spec.ExternalStorage.Enable, instance.Namespace, excludedAlertNames)
	if err != nil {
		r.Log.Error(err, "Prometheus rules file not found.")
		return err
	}
	err = mergo.Merge(&rule.Labels, instance.Spec.Monitoring.Labels, mergo.WithOverride)
	if err != nil {
		return err
	}
	err = r.CreateOrUpdatePrometheusRules(ctx, rule)
	if err != nil {
		r.Log.Error(err, "Unable to deploy Prometheus rules.")
		return err
	}
	return nil
}

func getPrometheusRules(isExternal bool, namespace string, excludedAlertNames []string) (*monitoringv1.PrometheusRule, error) {
	var err error
	if namespace == "" {
		return nil, fmt.Errorf("empty namespace passed")
	}
	rule := &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ruleName,
			Namespace: namespace,
		},
	}
	var ruleSpec *monitoringv1.PrometheusRuleSpec
	prometheusRuleFilePath := internalPrometheusRuleFilepath
	if isExternal {
		prometheusRuleFilePath = externalPrometheusRuleFilepath
	}
	ruleSpec, err = getPrometheusRuleSpecFrom(prometheusRuleFilePath)
	if err != nil {
		return nil, err
	}
	if len(excludedAlertNames) > 0 {
		ruleSpec.Groups = filterExcludedAlertsFromGroups(ruleSpec.Groups, excludedAlertNames)
	}
	rule.Spec = *ruleSpec
	return rule, nil
}

// filterExcludedAlertsFromGroups removes rules whose alert name is in excludedAlerts list
func filterExcludedAlertsFromGroups(groups []monitoringv1.RuleGroup, excludedAlerts []string) []monitoringv1.RuleGroup {
	excludedSet := make(map[string]bool)
	for _, alert := range excludedAlerts {
		excludedSet[alert] = true
	}

	filteredGroups := []monitoringv1.RuleGroup{}

	for _, group := range groups {
		var filteredRules []monitoringv1.Rule
		for _, rule := range group.Rules {
			// Check if this rule's alert name is in excluded list
			// Alert name is typically in rule.Alert field for alerting rules
			if rule.Alert != "" && excludedSet[rule.Alert] {
				// Skip this rule (it's excluded)
				continue
			}
			filteredRules = append(filteredRules, rule)
		}
		// Only include group if it has rules remaining
		if len(filteredRules) > 0 {
			group.Rules = filteredRules
			filteredGroups = append(filteredGroups, group)
		}
	}

	return filteredGroups
}

func getPrometheusRuleSpecFrom(filePath string) (*monitoringv1.PrometheusRuleSpec, error) {
	if err := CheckFileExists(filePath); err != nil {
		return nil, err
	}
	fileContent, err := os.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("'%s' not readable", filePath)
	}
	rule := monitoringv1.PrometheusRule{}
	if err := k8sYAML.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(fileContent)), 1000).Decode(&rule); err != nil {
		return nil, err
	}
	ruleSpec := rule.Spec
	return &ruleSpec, nil
}

// CheckFileExists checks for existence of file in given filepath
func CheckFileExists(filePath string) error {
	_, err := os.Stat(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("'%s' not found", filePath)
		}
		return err
	}
	return nil
}

// CreateOrUpdatePrometheusRules creates or updates Prometheus Rule
func (r *StorageClusterReconciler) CreateOrUpdatePrometheusRules(ctx context.Context, rule *monitoringv1.PrometheusRule) error {
	err := r.Create(ctx, rule)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			oldRule := &monitoringv1.PrometheusRule{}
			err = r.Get(ctx, types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, oldRule)
			if err != nil {
				return fmt.Errorf("failed while fetching PrometheusRule: %v", err)
			}
			oldRule.Spec = rule.Spec

			err = mergo.Merge(&oldRule.Labels, rule.Labels, mergo.WithOverride)
			if err != nil {
				return err
			}

			err := r.Update(ctx, oldRule)
			if err != nil {
				return fmt.Errorf("failed while updating PrometheusRule: %v", err)
			}
		} else {
			return fmt.Errorf("failed while creating PrometheusRule: %v", err)
		}
	}
	return nil
}
