package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/imdario/mergo"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
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
func (r *StorageClusterReconciler) enablePrometheusRules(instance *ocsv1.StorageCluster) error {
	rule, err := getPrometheusRules(instance.Spec.ExternalStorage.Enable)
	if err != nil {
		r.Log.Error(err, "Prometheus rules file not found.")
		return err
	}
	err = mergo.Merge(&rule.ObjectMeta.Labels, instance.Spec.Monitoring.Labels, mergo.WithOverride)
	if err != nil {
		return err
	}
	err = r.CreateOrUpdatePrometheusRules(rule)
	if err != nil {
		r.Log.Error(err, "Unable to deploy Prometheus rules.")
		return err
	}
	return nil
}

func getPrometheusRules(isExternal bool) (*monitoringv1.PrometheusRule, error) {
	rule := &monitoringv1.PrometheusRule{
		TypeMeta: metav1.TypeMeta{
			Kind:       monitoringv1.PrometheusRuleKind,
			APIVersion: monitoringv1.SchemeGroupVersion.String(),
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      ruleName,
			Namespace: os.Getenv(util.OperatorNamespaceEnvVar),
		},
	}
	var err error
	var ruleSpec *monitoringv1.PrometheusRuleSpec
	if isExternal {
		ruleSpec, err = getPrometheusRuleSpecFrom(externalPrometheusRuleFilepath)
		if err != nil {
			return nil, err
		}
	} else {
		ruleSpec, err = getPrometheusRuleSpecFrom(internalPrometheusRuleFilepath)
		if err != nil {
			return nil, err
		}
	}
	rule.Spec = *ruleSpec
	return rule, nil
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
func (r *StorageClusterReconciler) CreateOrUpdatePrometheusRules(rule *monitoringv1.PrometheusRule) error {
	err := r.Client.Create(context.TODO(), rule)
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			oldRule := &monitoringv1.PrometheusRule{}
			err = r.Client.Get(context.TODO(), types.NamespacedName{Name: rule.Name, Namespace: rule.Namespace}, oldRule)
			if err != nil {
				return fmt.Errorf("failed while fetching PrometheusRule: %v", err)
			}
			oldRule.Spec = rule.Spec

			err = mergo.Merge(&oldRule.Labels, rule.Labels, mergo.WithOverride)
			if err != nil {
				return err
			}

			err := r.Client.Update(context.TODO(), oldRule)
			if err != nil {
				return fmt.Errorf("failed while updating PrometheusRule: %v", err)
			}
		} else {
			return fmt.Errorf("failed while creating PrometheusRule: %v", err)
		}
	}
	return nil
}
