package defaults

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

var (
	// DaemonResources map contains the default resource requirements for the
	// various OCS daemons
	DaemonResources = map[string]corev1.ResourceRequirements{
		"noobaa-core": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("999m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("999m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		"noobaa-db": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("500m"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		"noobaa-db-vol": {
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("50Gi"),
			},
		},
		"noobaa-endpoint": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("999m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("999m"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		"nfs": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		"rbd-mirror": {
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		"ocs-metrics-exporter": {
			Requests: corev1.ResourceList{
				"memory": resource.MustParse("50Mi"),
				"cpu":    resource.MustParse("50m"),
			},
		},
	}

	LeanDaemonResources = map[string]corev1.ResourceRequirements{
		"mgr": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.5"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		"mon": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.5"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("0.5"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
		"osd": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1.5"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1.5"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
		"mds": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		"rgw": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1Gi"),
			},
		},
	}

	BalancedDaemonResources = map[string]corev1.ResourceRequirements{
		"mgr": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("1.5Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
		"mon": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
		"osd": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("5Gi"),
			},
		},
		"mds": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("6Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("6Gi"),
			},
		},
		"rgw": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
		},
	}

	PerformanceDaemonResources = map[string]corev1.ResourceRequirements{
		"mgr": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1.5"),
				corev1.ResourceMemory: resource.MustParse("2Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
		"mon": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1.5"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("1.5"),
				corev1.ResourceMemory: resource.MustParse("3Gi"),
			},
		},
		"osd": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		"mds": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("3"),
				corev1.ResourceMemory: resource.MustParse("8Gi"),
			},
		},
		"rgw": {
			Requests: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
			Limits: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("2"),
				corev1.ResourceMemory: resource.MustParse("4Gi"),
			},
		},
	}

	MonitoringResources = map[string]corev1.ResourceRequirements{
		"kube-rbac-proxy": {
			Requests: corev1.ResourceList{
				"memory": resource.MustParse("30Mi"),
				"cpu":    resource.MustParse("50m"),
			},
			Limits: corev1.ResourceList{
				"memory": resource.MustParse("30Mi"),
				"cpu":    resource.MustParse("50m"),
			},
		},
		"alertmanager": {
			Requests: corev1.ResourceList{
				"cpu":    resource.MustParse("100m"),
				"memory": resource.MustParse("200Mi"),
			},
			Limits: corev1.ResourceList{
				"cpu":    resource.MustParse("100m"),
				"memory": resource.MustParse("200Mi"),
			},
		},
		"prometheus": {
			Requests: corev1.ResourceList{
				"cpu":    resource.MustParse("400m"),
				"memory": resource.MustParse("250Mi"),
			},
			Limits: corev1.ResourceList{
				"cpu":    resource.MustParse("400m"),
				"memory": resource.MustParse("250Mi"),
			},
		},
	}
)
