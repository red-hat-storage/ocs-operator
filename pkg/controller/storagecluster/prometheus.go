package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	monitoringv1 "github.com/coreos/prometheus-operator/pkg/apis/monitoring/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
)

const (
	internalPrometheusRuleFilepath = "/ocs-prometheus-rules/prometheus-ocs-rules.yaml"
	externalPrometheusRuleFilepath = "/ocs-prometheus-rules/prometheus-ocs-rules-external.yaml"
	ruleName                       = "ocs-prometheus-rules"
	ruleNamespace                  = "openshift-storage"
)

// enablePrometheusRules is a wrapper around CreateOrUpdatePrometheusRule()
func (r *ReconcileStorageCluster) enablePrometheusRules(isExternal bool) error {
	rule, err := getPrometheusRules(isExternal)
	if err != nil {
		r.reqLogger.Error(err, "prometheus rules file not found")
	}
	err = r.CreateOrUpdatePrometheusRules(rule)
	if err != nil {
		r.reqLogger.Error(err, "unable to deploy Prometheus rules")
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
			Namespace: ruleNamespace,
		},
	}
	var err error
	ruleSpec := &monitoringv1.PrometheusRuleSpec{}
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
	fileContent, err := ioutil.ReadFile(filepath.Clean(filePath))
	if err != nil {
		return nil, fmt.Errorf("'%s' not readable", filePath)
	}
	ruleSpec := monitoringv1.PrometheusRuleSpec{}
	if err := k8sYAML.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(fileContent)), 1000).Decode(&ruleSpec); err != nil {
		return nil, err
	}
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
		}
		return fmt.Errorf("failed while creating PrometheusRule: %v", err)
	}
	return nil
}
