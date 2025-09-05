package templates

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

var ruleSelector = metav1.LabelSelector{
	MatchLabels: map[string]string{
		"prometheus": "rook-prometheus",
	},
}

var (
	AlertManagerEndpointName = "alertmanager-operated"
)

var PrometheusSpecTemplate = promv1.PrometheusSpec{
	CommonPrometheusFields: promv1.CommonPrometheusFields{
		ServiceAccountName:     "prometheus-k8s",
		ServiceMonitorSelector: &metav1.LabelSelector{},
		ListenLocal:            true,
		Resources:              defaults.MonitoringResources["prometheus"],
		Tolerations: []corev1.Toleration{{
			Key:      defaults.NodeTolerationKey,
			Operator: corev1.TolerationOpEqual,
			Value:    "true",
			Effect:   corev1.TaintEffectNoSchedule,
		}},
	},
	RuleSelector:   &ruleSelector,
	EnableAdminAPI: false,
	Alerting: &promv1.AlertingSpec{
		Alertmanagers: []promv1.AlertmanagerEndpoints{{
			Name: AlertManagerEndpointName,
			Port: intstr.FromString("web"),
		}},
	},
}
