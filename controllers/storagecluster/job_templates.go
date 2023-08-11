package storagecluster

import (
	"context"
	"fmt"
	"os"

	openshiftv1 "github.com/openshift/api/template/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

var extendClusterCommand = []string{
	"bash",
	"-c",
	`full_ratio_value=$(ceph osd dump | grep -m1 full_ratio | awk '{print $2}')
	  echo $full_ratio_value
	if [[ $full_ratio_value == "0.85"  ]]; then
		   ceph osd set-full-ratio 0.87
	else
		   ceph osd set-full-ratio 0.85
	fi
	`,
}

var osdCleanupArgs = []string{
	"ceph",
	"osd",
	"remove",
	"--osd-ids=${FAILED_OSD_IDS}",
	"--force-osd-removal",
	"${FORCE_OSD_REMOVAL}",
}

// ensureCreated ensures if the osd removal job template exists
func (obj *ocsJobTemplates) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {

	tempFuncs := []func(*ocsv1.StorageCluster) *openshiftv1.Template{
		osdCleanUpTemplate,
		extendClusterTemplate,
	}

	for _, tempFunc := range tempFuncs {
		template := tempFunc(sc)
		_, err := controllerutil.CreateOrUpdate(context.TODO(), r.Client, template, func() error {
			template.Parameters = tempFunc(sc).Parameters
			template.Objects = tempFunc(sc).Objects
			return controllerutil.SetControllerReference(sc, template, r.Scheme)
		})

		if err != nil && !errors.IsAlreadyExists(err) {
			return reconcile.Result{}, fmt.Errorf("failed to create Template : %v", err.Error())
		}

		// Delete a template job after the job gets completed
		// This eliminates the need to delete the older/existing job when triggering the job for a second time
		job := &batchv1.Job{}
		err = r.Client.Get(r.ctx, client.ObjectKey{Name: fmt.Sprintf("%s-job", template.Name), Namespace: template.Namespace}, job)
		if err == nil {
			if job.Status.Succeeded >= 1 {
				err = r.Client.Delete(r.ctx, job)
				if err != nil && !errors.IsNotFound(err) {
					r.Log.Error(err, "Job couldn't be deleted successfully after completion", "Job", klog.KRef(job.Namespace, job.Name))
				} else {
					r.Log.Info("Job deleted successfully after completion", "Job", klog.KRef(job.Namespace, job.Name))
				}
			}
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsJobTemplates
func (obj *ocsJobTemplates) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

func osdCleanUpTemplate(sc *ocsv1.StorageCluster) *openshiftv1.Template {

	jobTemplateName := "ocs-osd-removal"

	return &openshiftv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobTemplateName,
			Namespace: sc.Namespace,
		},
		Parameters: []openshiftv1.Parameter{
			{
				Name:        "FAILED_OSD_IDS",
				DisplayName: "OSD IDs",
				Required:    true,
				Description: `
The parameter OSD IDs needs a comma-separated list of numerical FAILED_OSD_IDs
when a single job removes multiple OSDs.
OSD removal is an advanced use case.
In the event of errors or invalid user inputs,
the Job will attempt to remove as many OSDs as can be processed and complete without returning an error condition.
Users should always check for errors and success in the log of the finished OSD removal Job.`,
			},
			{
				Name:        "FORCE_OSD_REMOVAL",
				DisplayName: "Force OSD Removal",
				Required:    false,
				Value:       "false",
				Description: `
A flag indicating whether the OSD should be forcefully removed. Valid values are true or false.
The default value is false. If an OSD is being removed that could lead to data loss, the OSD
will not be removed by default. If you see the osd removal fails and you are sure the OSD
should be removed, set this flag to true and run the job again.`,
			},
		},
		Objects: []runtime.RawExtension{
			{
				Object: newosdCleanUpJob(sc, jobTemplateName, osdCleanupArgs),
			},
		},
	}
}

func extendClusterTemplate(sc *ocsv1.StorageCluster) *openshiftv1.Template {

	jobTemplateName := "ocs-extend-cluster"

	return &openshiftv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobTemplateName,
			Namespace: sc.Namespace,
		},
		Parameters: []openshiftv1.Parameter{
			{
				Name:        "RECONFIGURE",
				DisplayName: "Ratio",
				Required:    false,
				Description: `
				 Currently this design does not require any parameter yet,
				 included for an additional possibility to implement something like
				 --increase-limit and --reset-limit`,
			},
		},
		Objects: []runtime.RawExtension{
			{
				Object: newExtendClusterJob(sc, jobTemplateName, extendClusterCommand),
			},
		},
	}
}

func newExtendClusterJob(sc *ocsv1.StorageCluster, jobTemplateName string, cephCommands []string) *batchv1.Job {
	labels := map[string]string{
		"app": "ceph-toolbox-job",
	}

	// Annotation template.alpha.openshift.io/wait-for-ready ensures template readiness
	annotations := map[string]string{
		"template.alpha.openshift.io/wait-for-ready": "true",
	}

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobTemplateName + "-job",
			Namespace:   sc.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{

					InitContainers: []corev1.Container{
						{
							Name:            "config-init",
							Image:           os.Getenv("ROOK_CEPH_IMAGE"),
							Command:         []string{"/usr/local/bin/toolbox.sh"},
							Args:            []string{"--skip-watch"},
							ImagePullPolicy: "IfNotPresent",
							Env: []corev1.EnvVar{
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
									Name:  "POD_NAMESPACE",
									Value: sc.Namespace,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ceph-config",
									MountPath: "/etc/ceph",
								},
								{
									Name:      "mon-endpoint-volume",
									MountPath: "/etc/rook",
								},
							},
						},
					},

					Containers: []corev1.Container{
						{
							Name:  "script",
							Image: os.Getenv("ROOK_CEPH_IMAGE"),
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ceph-config",
									MountPath: "/etc/ceph",
									ReadOnly:  true,
								},
							},
							Command: cephCommands,
						},
					},
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: "default",
					Volumes: []corev1.Volume{
						{
							Name:         "rook-config",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name:         "ceph-config",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name: "mon-endpoint-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
									Items: []corev1.KeyToPath{
										{
											Key:  "data",
											Path: "mon-endpoints",
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
	return job

}

func newosdCleanUpJob(sc *ocsv1.StorageCluster, jobTemplateName string, cephCommands []string) *batchv1.Job {
	labels := map[string]string{
		"app": "ceph-toolbox-job",
	}

	// Annotation template.alpha.openshift.io/wait-for-ready ensures template readiness
	annotations := map[string]string{
		"template.alpha.openshift.io/wait-for-ready": "true",
	}

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        jobTemplateName + "-job",
			Namespace:   sc.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: "rook-ceph-system",
					Volumes: []corev1.Volume{
						{
							Name:         "ceph-conf-emptydir",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name:         "rook-config",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},

					Containers: []corev1.Container{
						{
							Name:  "operator",
							Image: os.Getenv("ROOK_CEPH_IMAGE"),
							Args:  cephCommands,
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ceph-conf-emptydir",
									MountPath: "/etc/ceph",
								},
								{
									Name:      "rook-config",
									MountPath: "/var/lib/rook",
								},
							},
							Env: []corev1.EnvVar{
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
									Name:  "POD_NAMESPACE",
									Value: sc.Namespace,
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
								{
									Name:  "ROOK_CONFIG_DIR",
									Value: "/var/lib/rook",
								},
								{
									Name:  "ROOK_CEPH_CONFIG_OVERRIDE",
									Value: "/etc/rook/config/override.conf",
								},
								{
									Name:  "ROOK_LOG_LEVEL",
									Value: "DEBUG",
								},
							},
						},
					},
				},
			},
		},
	}

	return job
}
