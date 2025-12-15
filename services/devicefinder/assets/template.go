package assets

import (
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// NewDeviceFinderDaemonSet creates a DaemonSet object for device discovery
func NewDeviceFinderDaemonSet(namespace string, containerImage string) *appsv1.DaemonSet {
	return &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "devicefinder-discovery",
			Namespace: namespace,
			Labels: map[string]string{
				"app": "devicefinder-discovery",
			},
		},
		Spec: appsv1.DaemonSetSpec{
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "devicefinder-discovery",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "devicefinder-discovery",
					},
					Annotations: map[string]string{
						"target.workload.openshift.io/management": `{"effect": "PreferredDuringScheduling"}`,
					},
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: "ocs-devicefinder-sa",
					PriorityClassName:  "system-node-critical",
					Containers: []corev1.Container{
						{
							Name:  "devicefinder-discovery",
							Image: containerImage,
							Args:  []string{"discover"},
							Env: []corev1.EnvVar{
								{
									Name: "NODE_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "spec.nodeName",
										},
									},
								},
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.namespace",
										},
									},
								},
								{
									Name: "POD_NAME",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.name",
										},
									},
								},
								{
									Name: "POD_UID",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											APIVersion: "v1",
											FieldPath:  "metadata.uid",
										},
									},
								},
							},
							ImagePullPolicy: corev1.PullAlways,
							SecurityContext: &corev1.SecurityContext{
								Privileged: func() *bool { b := true; return &b }(),
							},
							Resources: corev1.ResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceMemory: resource.MustParse("50Mi"),
									corev1.ResourceCPU:    resource.MustParse("10m"),
								},
							},
							TerminationMessagePath:   "/dev/termination-log",
							TerminationMessagePolicy: corev1.TerminationMessageFallbackToLogsOnError,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:             "device-dir",
									MountPath:        "/dev",
									MountPropagation: func() *corev1.MountPropagationMode { m := corev1.MountPropagationHostToContainer; return &m }(),
								},
								{
									Name:             "run-udev",
									MountPath:        "/run/udev",
									MountPropagation: func() *corev1.MountPropagationMode { m := corev1.MountPropagationHostToContainer; return &m }(),
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "device-dir",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/dev",
									Type: func() *corev1.HostPathType { t := corev1.HostPathDirectory; return &t }(),
								},
							},
						},
						{
							Name: "run-udev",
							VolumeSource: corev1.VolumeSource{
								HostPath: &corev1.HostPathVolumeSource{
									Path: "/run/udev",
								},
							},
						},
					},
				},
			},
		},
	}
}
