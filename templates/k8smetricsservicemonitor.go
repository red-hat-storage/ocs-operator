package templates

import (
	promv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var params = map[string][]string{
	"match[]": {
		"{__name__=\"kube_node_status_condition\"}",
		"{__name__=\"kube_persistentvolume_info\"}",
		"{__name__=\"kube_storageclass_info\"}",
		"{__name__=\"kube_persistentvolumeclaim_info\"}",
		"{__name__=\"kube_deployment_spec_replicas\"}",
		"{__name__=\"kube_pod_status_phase\"}",
		"{__name__=\"kubelet_volume_stats_capacity_bytes\"}",
		"{__name__=\"kubelet_volume_stats_used_bytes\"}",
		"{__name__=\"node_disk_read_time_seconds_total\"}",
		"{__name__=\"node_disk_write_time_seconds_total\"}",
		"{__name__=\"node_disk_reads_completed_total\"}",
		"{__name__=\"node_disk_writes_completed_total\"}",
	},
}

var K8sMetricsServiceMonitorSpecTemplate = promv1.ServiceMonitorSpec{

	Endpoints: []promv1.Endpoint{
		{
			Port:          "web",
			Path:          "/federate",
			Scheme:        "https",
			ScrapeTimeout: "1m",
			Interval:      "2m",
			HonorLabels:   true,
			MetricRelabelConfigs: []promv1.RelabelConfig{
				{
					Action: "labeldrop",
					Regex:  "prometheus_replica",
				},
			},
			RelabelConfigs: []promv1.RelabelConfig{
				{
					Action:      "replace",
					Regex:       "prometheus-k8s-.*",
					Replacement: ptr.To(""),
					SourceLabels: []promv1.LabelName{
						"pod",
					},
					TargetLabel: "pod",
				},
			},
			TLSConfig: &promv1.TLSConfig{
				SafeTLSConfig: promv1.SafeTLSConfig{
					InsecureSkipVerify: ptr.To(false),
					ServerName:         ptr.To("prometheus-k8s.openshift-monitoring.svc"),
				},
				CAFile: "/var/run/secrets/kubernetes.io/serviceaccount/service-ca.crt",
			},
			Params:          params,
			BearerTokenFile: "/var/run/secrets/kubernetes.io/serviceaccount/token",
		},
	},
	NamespaceSelector: promv1.NamespaceSelector{
		MatchNames: []string{"openshift-monitoring"},
	},
	Selector: metav1.LabelSelector{
		MatchLabels: map[string]string{
			"app.kubernetes.io/component": "prometheus",
		},
	},
}
