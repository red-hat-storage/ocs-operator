package templates

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"k8s.io/utils/ptr"
)

var AlertmanagerSpecTemplate = promv1.AlertmanagerSpec{
	Replicas:  ptr.To(int32(1)),
	Resources: defaults.MonitoringResources["alertmanager"],
}
