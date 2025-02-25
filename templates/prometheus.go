package templates

import (
	"fmt"
	"os"

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
	KubeRBACProxyPortNumber              = 9339
	PrometheusKubeRBACProxyConfigMapName = "prometheus-kube-rbac-proxy-config"
	AlertManagerEndpointName             = "alertmanager-operated"
)

var PrometheusSpecTemplate = promv1.PrometheusSpec{
	CommonPrometheusFields: promv1.CommonPrometheusFields{
		ServiceAccountName:     "prometheus-k8s",
		ServiceMonitorSelector: &metav1.LabelSelector{},
		ListenLocal:            true,
		Resources:              defaults.MonitoringResources["prometheus"],
		Containers: []corev1.Container{{
			Name:  "kube-rbac-proxy",
			Image: os.Getenv("KUBE_RBAC_PROXY_IMAGE"),
			Args: []string{
				fmt.Sprintf("--secure-listen-address=0.0.0.0:%d", KubeRBACProxyPortNumber),
				"--upstream=http://127.0.0.1:9090/",
				"--logtostderr=true",
				"--v=10",
				"--tls-cert-file=/etc/tls-secret/tls.crt",
				"--tls-private-key-file=/etc/tls-secret/tls.key",
				"--client-ca-file=/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt",
				"--config-file=/etc/kube-rbac-config/config-file.json",
			},
			Ports: []corev1.ContainerPort{{
				Name:          "https",
				ContainerPort: int32(KubeRBACProxyPortNumber),
			}},
			VolumeMounts: []corev1.VolumeMount{
				{
					Name:      "serving-cert",
					MountPath: "/etc/tls-secret",
				},
				{
					Name:      "kube-rbac-config",
					MountPath: "/etc/kube-rbac-config",
				},
			},
			Resources: defaults.MonitoringResources["kube-rbac-proxy"],
		}},
		Volumes: []corev1.Volume{
			{
				Name: "serving-cert",
				VolumeSource: corev1.VolumeSource{
					Secret: &corev1.SecretVolumeSource{
						SecretName: "prometheus-serving-cert-secret",
					},
				},
			},
			{
				Name: "kube-rbac-config",
				VolumeSource: corev1.VolumeSource{
					ConfigMap: &corev1.ConfigMapVolumeSource{
						LocalObjectReference: corev1.LocalObjectReference{
							Name: PrometheusKubeRBACProxyConfigMapName,
						},
					},
				},
			},
		},
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
