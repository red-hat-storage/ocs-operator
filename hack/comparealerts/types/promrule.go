package types

import (
	"comparealerts/utils"
	"errors"
	"os"

	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
)

type PrometheusRule struct {
	metav1.TypeMeta   `yaml:",inline"`
	metav1.ObjectMeta `yaml:"metadata,omitempty"`
	Spec              PrometheusRuleSpec `yaml:"spec"`
	fromFile          string
}
type PrometheusRuleSpec struct {
	Groups []RuleGroup `yaml:"groups,omitempty"`
}
type RuleGroup struct {
	Name                    string `yaml:"name"`
	Interval                string `yaml:"interval,omitempty"`
	Rules                   []Rule `yaml:"rules"`
	PartialResponseStrategy string `yaml:"partial_response_strategy,omitempty"`
}
type Rule struct {
	Record      string            `yaml:"record,omitempty"`
	Alert       string            `yaml:"alert,omitempty"`
	Expr        IntOrString       `yaml:"expr"`
	For         string            `yaml:"for,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
}

func NewPrometheusRuleFromFile(fileName string) (*PrometheusRule, error) {
	promRuleFile, err := os.Open(fileName)
	if err != nil {
		return nil, err
	}
	var promRule *PrometheusRule
	promRule, err = parseFromYaml(fileName, promRuleFile)
	if err != nil {
		promRule, err = parseFromJson(fileName, promRuleFile)
	}
	if err != nil {
		err = errors.New("corrupted yaml or json file: " + fileName)
		return nil, err
	}
	promRule.SetFromFile(fileName)
	return promRule, err
}

func (promRule *PrometheusRule) FromFile() string {
	return promRule.fromFile
}

func (promRule *PrometheusRule) SetFromFile(fromFile string) {
	// cannot set a new source file name if the object is already populated
	if promRule.fromFile != "" && len(promRule.Spec.Groups) > 0 {
		klog.Warningf("Object already populated, cannot set a new file name")
		return
	}
	promRule.fromFile = fromFile
}

func (promRule *PrometheusRule) Alerts() map[string][]Rule {
	if promRule == nil {
		return nil
	}
	allAlerts := make(map[string][]Rule)
	for _, eachRuleGroup := range promRule.Spec.Groups {
	topRuleLabel:
		for _, topRule := range eachRuleGroup.Rules {
			if topRule.Alert == "" {
				continue
			}
			if collectedRules, ok := allAlerts[topRule.Alert]; ok {
				for _, collectedRule := range collectedRules {
					if collectedRule.Expr == topRule.Expr {
						continue topRuleLabel
					}
				}
				klog.Warningf("Multiple entries found for alert: %q", topRule.Alert)
			}
			allAlerts[topRule.Alert] = append(allAlerts[topRule.Alert], topRule)
		}
	}
	return allAlerts
}

func (promRule *PrometheusRule) Diff(promRule2 *PrometheusRule) []PrometheusRuleDiffUnit {
	prom1Alerts := promRule.Alerts()
	prom2Alerts := promRule2.Alerts()
	var diffs []PrometheusRuleDiffUnit
	for alert1Name, alert1Rules := range prom1Alerts {
		var diffsInAlert1Name = PrometheusRuleDiffUnit{Alert: alert1Name}
		if alert2Rules, ok := prom2Alerts[alert1Name]; !ok {
			// first check: see if the alert is only with the first 'PrometheusRule' object
			diffRSubUnit := NewDiffReasonSubUnit(AlertOnlyWithMe,
				AlertOnlyWithMe.String(), alert1Rules, nil)
			diffsInAlert1Name.DiffReasons = append(diffsInAlert1Name.DiffReasons, diffRSubUnit)
		} else if len(alert1Rules) != len(alert2Rules) {
			// second check: whether the rules array have different length
			diffRSubUnit := NewDiffReasonSubUnit(DifferenceInNoOfRules,
				DifferenceInNoOfRules.String(), alert1Rules, alert2Rules)
			diffsInAlert1Name.DiffReasons = append(diffsInAlert1Name.DiffReasons, diffRSubUnit)
		} else {
			// third check:
			// we established: (a) both have the same alert with (b) same #:of rules
			// now check: those rules have same expressions or not
			alert1RuleExprSame := false
		rule1Loop:
			for _, ruleInAlert1 := range alert1Rules {
				ruleInAlert1ExprTrimmed := utils.TrimWithOnlySpaces(ruleInAlert1.Expr.IntOrString.String())
				for _, ruleInAlert2 := range alert2Rules {
					ruleInAlert2ExprTrimmed := utils.TrimWithOnlySpaces(ruleInAlert2.Expr.IntOrString.String())
					if ruleInAlert1ExprTrimmed == ruleInAlert2ExprTrimmed {
						alert1RuleExprSame = true
						break rule1Loop
					}
				}
			}
			if !alert1RuleExprSame {
				diffRSubUnit := NewDiffReasonSubUnit(DifferentExpr, DifferentExpr.String(),
					alert1Rules, alert2Rules)
				diffsInAlert1Name.DiffReasons = append(diffsInAlert1Name.DiffReasons, diffRSubUnit)
			}

		}
		if len(diffsInAlert1Name.DiffReasons) > 0 {
			diffs = append(diffs, diffsInAlert1Name)
		}
	}
	for alert2Name := range prom2Alerts {
		var diffsInAlert2Name = PrometheusRuleDiffUnit{Alert: alert2Name}
		if _, ok := prom1Alerts[alert2Name]; !ok {
			diffRSubUnit := NewDiffReasonSubUnit(AlertOnlyWithThem, AlertOnlyWithThem.String(),
				nil, prom2Alerts[alert2Name])
			diffsInAlert2Name.DiffReasons = append(diffsInAlert2Name.DiffReasons, diffRSubUnit)
			diffs = append(diffs, diffsInAlert2Name)
			continue
		}
	}
	return diffs
}

type IntOrString struct {
	intstr.IntOrString
	LineNo int
}

func (iOrS *IntOrString) UnmarshalYAML(value *yaml.Node) error {
	iOrS.LineNo = value.Line
	iOrS.IntOrString = intstr.Parse(value.Value)
	return nil
}
