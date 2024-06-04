package operations

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

const (
	PendingOperationsAnnotation = "ops.ocs.openshift.io/pending-ops"
)

func GetJob(op string, opts ...string) (*batchv1.Job, error) {
	var job *batchv1.Job

	switch op {
	case "volume-migration":
		job = getVolumeMigrationJob(opts...)
	}

	return job, nil
}

func getVolumeMigrationJob(opts ...string) *batchv1.Job {
	mode := opts[0]

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Labels: map[string]string{
				"app": "ocs-volume-migration",
			},
			Name: "volume-migration-" + mode,
		},
		Spec: batchv1.JobSpec{
			BackoffLimit: ptr.To(int32(100)),
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyOnFailure,
					ServiceAccountName: "ocs-operator",
					Containers: []corev1.Container{
						{
							Name:            "volume-migration",
							Image:           "quay.io/ocs-dev/ocs-volume-migration:latest",
							ImagePullPolicy: corev1.PullAlways,
							Command:         []string{"volume-migration"},
							Args: []string{
								"-m", mode,
								"-f", "state.json",
							},
							Env: []corev1.EnvVar{
								{
									Name: "POD_NAMESPACE",
									ValueFrom: &corev1.EnvVarSource{
										FieldRef: &corev1.ObjectFieldSelector{
											FieldPath: "metadata.namespace",
										},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	if mode == "converged" || mode == "provider" {
		job.Spec.Template.Spec.InitContainers = []corev1.Container{getCephToolboxContainer()}
		job.Spec.Template.Spec.Volumes = getCephToolboxVolumes()
	}

	return job
}

func getCephToolboxContainer() corev1.Container {
	toolboxContainer := corev1.Container{
		Name:            "config-init",
		Image:           "quay.io/ocs-dev/ocs-volume-migration:latest",
		Command:         []string{"/usr/local/bin/toolbox.sh"},
		Args:            []string{"--skip-watch"},
		ImagePullPolicy: corev1.PullIfNotPresent,
		Env: []corev1.EnvVar{
			{
				Name: "POD_NAMESPACE",
				ValueFrom: &corev1.EnvVarSource{
					FieldRef: &corev1.ObjectFieldSelector{
						FieldPath: "metadata.namespace",
					},
				},
			},
			{
				Name: "ROOK_MON_ENDPOINTS",
				ValueFrom: &corev1.EnvVarSource{
					ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
						Key:                  "data",
						LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
					},
				},
			},
			{
				Name: "ROOK_CEPH_USERNAME",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  "ceph-username",
						LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
					},
				},
			},
			{
				Name: "ROOK_CEPH_SECRET",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  "ceph-secret",
						LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
					},
				},
			},
			{
				Name: "ROOK_FSID",
				ValueFrom: &corev1.EnvVarSource{
					SecretKeyRef: &corev1.SecretKeySelector{
						Key:                  "fsid",
						LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
					},
				},
			},
			{Name: "ROOK_CONFIG_DIR", Value: "/var/lib/rook"},
			{Name: "ROOK_CEPH_CONFIG_OVERRIDE", Value: "/etc/rook/config/override.conf"},
			{Name: "ROOK_LOG_LEVEL", Value: "DEBUG"},
		},
		VolumeMounts: []corev1.VolumeMount{
			{Name: "ceph-config", MountPath: "/etc/ceph"},
			{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
		},
		SecurityContext: &corev1.SecurityContext{
			AllowPrivilegeEscalation: ptr.To(false),
			Capabilities: &corev1.Capabilities{
				Drop: []corev1.Capability{
					"ALL",
				},
			},
		},
	}
	return toolboxContainer
}

func getCephToolboxVolumes() []corev1.Volume {
	volumes := []corev1.Volume{
		{
			Name:         "ceph-config",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name:         "rook-config",
			VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		},
		{
			Name: "mon-endpoint-volume",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
					Items:                []corev1.KeyToPath{{Key: "data", Path: "mon-endpoints"}},
				},
			},
		},
	}

	return volumes
}
