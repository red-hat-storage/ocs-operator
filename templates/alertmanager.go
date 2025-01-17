package templates

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
)

var AlertmanagerSpecTemplate = promv1.AlertmanagerSpec{
	Replicas:  ptr.To(int32(1)),
	Resources: defaults.MonitoringResources["alertmanager"],
	Tolerations: []corev1.Toleration{{
		Key:      defaults.NodeTolerationKey,
		Operator: corev1.TolerationOpEqual,
		Value:    "true",
		Effect:   corev1.TaintEffectNoSchedule,
	}},
}
