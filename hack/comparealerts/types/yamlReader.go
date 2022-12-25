package types

import (
	"encoding/json"
	"fmt"
	"io"

	monv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"gopkg.in/yaml.v3"
)

func parseFromJson(sourceName string, jsonFile io.Reader) (*PrometheusRule, error) {
	promRuleDecoder := json.NewDecoder(jsonFile)
	var origPromRule = &monv1.PrometheusRule{}
	if err := promRuleDecoder.Decode(origPromRule); err != nil {
		return nil, fmt.Errorf("unable to decode json from  %q: %w", sourceName, err)
	}
	localPromRule := convertToLocalPrometheusRule(origPromRule)
	return localPromRule, nil
}

func convertToLocalPrometheusRule(origPromRule *monv1.PrometheusRule) *PrometheusRule {
	localPromRule := &PrometheusRule{}
	localPromRule.TypeMeta = origPromRule.TypeMeta
	localPromRule.ObjectMeta = *origPromRule.ObjectMeta.DeepCopy()
	for _, ruleGroup := range origPromRule.Spec.Groups {
		localRuleGroup := RuleGroup{
			Name:                    ruleGroup.Name,
			Interval:                ruleGroup.Interval,
			PartialResponseStrategy: ruleGroup.PartialResponseStrategy,
		}
		var localRules []Rule
		for _, rule := range ruleGroup.Rules {
			ruleCopy := *rule.DeepCopy()
			localRule := Rule{
				Record:      ruleCopy.Record,
				Alert:       ruleCopy.Alert,
				Expr:        IntOrString{IntOrString: ruleCopy.Expr},
				For:         ruleCopy.For,
				Labels:      ruleCopy.Labels,
				Annotations: ruleCopy.Annotations,
			}
			localRules = append(localRules, localRule)
		}
		localRuleGroup.Rules = localRules
		localPromRule.Spec.Groups = append(localPromRule.Spec.Groups, localRuleGroup)
	}
	return localPromRule
}

func parseFromYaml(sourceName string, yamlFile io.Reader) (*PrometheusRule, error) {
	promRuleDecoder := yaml.NewDecoder(yamlFile)
	promRule := &PrometheusRule{}
	if err := promRuleDecoder.Decode(promRule); err != nil {
		return nil, fmt.Errorf("unable to decode yaml from %q: %w", sourceName, err)
	}
	return promRule, nil
}
