package ocsinitialization

import (
	"context"
	"reflect"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const rookCephToolDeploymentName = "rook-ceph-tools"

func newToolsDeployment(namespace string, rookImage string) *appsv1.Deployment {

	name := rookCephToolDeploymentName
	var replicaOne int32 = 1

	privilegedContainer := true
	return &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicaOne,
			Selector: &metav1.LabelSelector{
				MatchLabels: map[string]string{
					"app": "rook-ceph-tools",
				},
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: map[string]string{
						"app": "rook-ceph-tools",
					},
				},
				Spec: corev1.PodSpec{
					DNSPolicy: corev1.DNSClusterFirstWithHostNet,
					Containers: []corev1.Container{
						{
							Name:    name,
							Image:   rookImage,
							Command: []string{"/tini"},
							Args:    []string{"-g", "--", "/usr/local/bin/toolbox.sh"},
							Env: []corev1.EnvVar{
								{
									Name: "ROOK_CEPH_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-username",
										},
									},
								},
								{
									Name: "ROOK_CEPH_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-secret",
										},
									},
								},
							},
							SecurityContext: &corev1.SecurityContext{
								Privileged: &privilegedContainer,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "dev", MountPath: "/dev"},
								{Name: "sysbus", MountPath: "/sys/bus"},
								{Name: "libmodules", MountPath: "/lib/modules"},
								{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						{
							Key:      defaults.NodeTolerationKey,
							Operator: corev1.TolerationOpEqual,
							Value:    "true",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					// if hostNetwork: false, the "rbd map" command hangs, see https://github.com/rook/rook/issues/2021
					HostNetwork: true,
					Volumes: []corev1.Volume{
						{Name: "dev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev"}}},
						{Name: "sysbus", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys/bus"}}},
						{Name: "libmodules", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/lib/modules"}}},
						{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items: []corev1.KeyToPath{
									{Key: "data", Path: "mon-endpoints"},
								},
							},
						},
						},
					},
				},
			},
		},
	}
}

func (r *OCSInitializationReconciler) ensureToolsDeployment(initialData *ocsv1.OCSInitialization) error {

	var isFound bool
	namespace := initialData.Namespace

	toolsDeployment := newToolsDeployment(namespace, r.RookImage)
	foundToolsDeployment := &appsv1.Deployment{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: namespace}, foundToolsDeployment)

	if err == nil {
		isFound = true
	} else if errors.IsNotFound(err) {
		isFound = false
	} else {
		return err
	}

	if initialData.Spec.EnableCephTools {
		// Create or Update if ceph tools is enabled.

		if !isFound {
			return r.Client.Create(context.TODO(), toolsDeployment)
		} else if !reflect.DeepEqual(foundToolsDeployment.Spec, toolsDeployment.Spec) {

			updateDeployment := foundToolsDeployment.DeepCopy()
			updateDeployment.Spec = *toolsDeployment.Spec.DeepCopy()

			return r.Client.Update(context.TODO(), updateDeployment)
		}
	} else if isFound {
		// delete if ceph tools exists and is disabled
		return r.Client.Delete(context.TODO(), foundToolsDeployment)
	}

	return nil
}
