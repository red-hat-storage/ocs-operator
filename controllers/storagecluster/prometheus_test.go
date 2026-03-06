package storagecluster

import (
	"testing"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestFilterDisabledAlertsFromGroups(t *testing.T) {
	tests := []struct {
		name           string
		groups         []monitoringv1.RuleGroup
		disabledAlerts []string
		expected       []monitoringv1.RuleGroup
	}{
		{
			name: "no alerts disabled - return all rules",
			groups: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFNodeMTULessThan9000", Expr: intstr.FromString("expr2")},
						{Alert: "ODFDiskUtilizationHigh", Expr: intstr.FromString("expr3")},
					},
				},
			},
			disabledAlerts: []string{},
			expected: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFNodeMTULessThan9000", Expr: intstr.FromString("expr2")},
						{Alert: "ODFDiskUtilizationHigh", Expr: intstr.FromString("expr3")},
					},
				},
			},
		},
		{
			name: "disable one alert - remove from list",
			groups: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFNodeMTULessThan9000", Expr: intstr.FromString("expr2")},
						{Alert: "ODFDiskUtilizationHigh", Expr: intstr.FromString("expr3")},
					},
				},
			},
			disabledAlerts: []string{"ODFNodeMTULessThan9000"},
			expected: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFDiskUtilizationHigh", Expr: intstr.FromString("expr3")},
					},
				},
			},
		},
		{
			name: "disable multiple alerts - remove all matches",
			groups: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFNodeLatencyHighOnNonOSDNodes", Expr: intstr.FromString("expr2")},
						{Alert: "ODFNodeMTULessThan9000", Expr: intstr.FromString("expr3")},
						{Alert: "ODFNodeNICBandwidthSaturation", Expr: intstr.FromString("expr4")},
					},
				},
			},
			disabledAlerts: []string{
				"ODFNodeLatencyHighOnOSDNodes",
				"ODFNodeMTULessThan9000",
			},
			expected: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnNonOSDNodes", Expr: intstr.FromString("expr2")},
						{Alert: "ODFNodeNICBandwidthSaturation", Expr: intstr.FromString("expr4")},
					},
				},
			},
		},
		{
			name: "disable all alerts - return empty groups",
			groups: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
						{Alert: "ODFCorePodRestarted", Expr: intstr.FromString("expr2")},
					},
				},
			},
			disabledAlerts: []string{
				"ODFNodeLatencyHighOnOSDNodes",
				"ODFCorePodRestarted",
			},
			expected: []monitoringv1.RuleGroup{},
		},
		{
			name:           "empty input groups - return empty",
			groups:         []monitoringv1.RuleGroup{},
			disabledAlerts: []string{"any_alert"},
			expected:       []monitoringv1.RuleGroup{},
		},
		{
			name: "nil disabled alerts - return all rules",
			groups: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
					},
				},
			},
			disabledAlerts: nil,
			expected: []monitoringv1.RuleGroup{
				{
					Name: "odf_healthchecks.rules",
					Rules: []monitoringv1.Rule{
						{Alert: "ODFNodeLatencyHighOnOSDNodes", Expr: intstr.FromString("expr1")},
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := filterDisabledAlertsFromGroups(tt.groups, tt.disabledAlerts)
			assert.Equal(t, tt.expected, result)
		})
	}
}
