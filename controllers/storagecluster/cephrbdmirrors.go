package storagecluster

import (
	"context"
	"fmt"
	"os"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephRbdMirrors struct{}

var (
	rbdMirrorDebugLoggingEnabled = false
)

func (r *StorageClusterReconciler) fetchCephRbdMirrorInstance(cephRbdMirror *cephv1.CephRBDMirror) (cephv1.CephRBDMirror, error) {
	existing := cephv1.CephRBDMirror{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephRbdMirror.Name, Namespace: cephRbdMirror.Namespace}, &existing)
	return existing, err
}

func (r *StorageClusterReconciler) deleteCephRbdMirrorInstance(cephRbdMirrors []*cephv1.CephRBDMirror) error {
	for _, cephRbdMirror := range cephRbdMirrors {
		existing, err := r.fetchCephRbdMirrorInstance(cephRbdMirror)
		if err != nil {
			if !errors.IsNotFound(err) {
				return fmt.Errorf("failed to get CephRbdMirror %v: %v", cephRbdMirror.Name, err)
			}
			continue
		}
		if cephRbdMirror.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Deleting CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
			err := r.Client.Delete(context.TODO(), &existing)
			if err != nil {
				r.Log.Error(err, "Failed to delete CephRbdMirror.", "CephRbdMirror", klog.KRef(existing.Namespace, existing.Name))
				return fmt.Errorf("failed to delete CephRbdMirror %v: %v", existing.Name, err)
			}
			r.Log.Info("CephRbdMirror is deleted.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
		}
	}
	return nil
}

// newCephRbdMirrorInstances returns the cephRbdMirror instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephRbdMirrorInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephRBDMirror, error) {
	ret := []*cephv1.CephRBDMirror{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephRbdMirror(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.RBDMirroringSpec{
				Count:     1,
				Resources: defaults.GetDaemonResources("rbd-mirror", initData.Spec.Resources),
				Placement: getPlacement(initData, "rbd-mirror"),
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set controller reference for CephRBDMirror.", "CephRBDMirror", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}
	return ret, nil
}

// ensureCreated ensures that cephRbdMirror resources exist in the desired state.
func (obj *ocsCephRbdMirrors) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephRBDMirror.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	cephRbdMirrors, err := r.newCephRbdMirrorInstances(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	if !instance.Spec.Mirroring.Enabled {
		return reconcile.Result{}, r.deleteCephRbdMirrorInstance(cephRbdMirrors)
	}

	for _, cephRbdMirror := range cephRbdMirrors {
		existing, err := r.fetchCephRbdMirrorInstance(cephRbdMirror)

		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Creating CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
				err = r.Client.Create(context.TODO(), cephRbdMirror)
				if err != nil {
					r.Log.Error(err, "Failed to create CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
					return reconcile.Result{}, err
				}
				continue
			}
			r.Log.Error(err, "Failed to get CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
			return reconcile.Result{}, err
		}

		if existing.DeletionTimestamp != nil {
			r.Log.Info("Unable to restore CephRbdMirror, It is marked for deletion.", "CephRbdMirror", klog.KRef(existing.Namespace, existing.Name))
			return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %s, It is marked for deletion", existing.Name)
		}

		r.Log.Info("Restoring original CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
		existing.ObjectMeta.OwnerReferences = cephRbdMirror.ObjectMeta.OwnerReferences
		cephRbdMirror.ObjectMeta = existing.ObjectMeta
		err = r.Client.Update(context.TODO(), cephRbdMirror)
		if err != nil {
			r.Log.Error(err, "Failed to update CephRbdMirror.", "CephRbdMirror", klog.KRef(cephRbdMirror.Namespace, cephRbdMirror.Name))
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephRbdMirrors owned by the StorageCluster
func (obj *ocsCephRbdMirrors) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	cephRbdMirrors, err := r.newCephRbdMirrorInstances(sc)
	if err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, r.deleteCephRbdMirrorInstance(cephRbdMirrors)
}

func (r *StorageClusterReconciler) ensureRbdMirrorDebugLogging(sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	enableRbdMirrorDebugLoggingJobName := "enable-rbd-mirror-debug-logging"
	disableRbdMirrorDebugLoggingJobName := "disable-rbd-mirror-debug-logging"
	enableRbdMirrorDebugLoggingCommands :=
		`echo "starting configuration of mirror logging"
ceph config set client.rbd-mirror.a debug_ms 1
ceph config set client.rbd-mirror.a debug_rbd 15
ceph config set client.rbd-mirror.a debug_rbd_mirror 30
ceph config set client.rbd-mirror.a log_file /var/log/ceph/\$cluster-\$name.log
ceph config set client.rbd-mirror-peer debug_ms 1
ceph config set client.rbd-mirror-peer debug_rbd 15
ceph config set client.rbd-mirror-peer debug_rbd_mirror 30
ceph config set client.rbd-mirror-peer log_file /var/log/ceph/\$cluster-\$name.log
ceph config set mgr mgr/rbd_support/log_level debug
		echo "completed"`
	disableRbdMirrorDebugLoggingCommands :=
		`echo "Removing configuration of mirror logging"
ceph config rm client.rbd-mirror.a debug_ms
ceph config rm client.rbd-mirror.a debug_rbd
ceph config rm client.rbd-mirror.a debug_rbd_mirror
ceph config rm client.rbd-mirror.a log_file
ceph config rm client.rbd-mirror-peer debug_ms
ceph config rm client.rbd-mirror-peer debug_rbd
ceph config rm client.rbd-mirror-peer debug_rbd_mirror
ceph config rm client.rbd-mirror-peer log_file
ceph config rm mgr mgr/rbd_support/log_level
		echo "completed"`

	// Mirroring is enabled but the debug logging is disabled
	if sc.Spec.Mirroring.Enabled && !rbdMirrorDebugLoggingEnabled {

		err := r.Client.Create(r.ctx, getRbdMirrorDebugLoggingJob(sc, enableRbdMirrorDebugLoggingJobName, enableRbdMirrorDebugLoggingCommands))
		if err != nil && !errors.IsAlreadyExists(err) {
			return reconcile.Result{}, err
		}
		// Check if the job has succeeded
		job := &batchv1.Job{}
		err = r.Client.Get(r.ctx, types.NamespacedName{Name: enableRbdMirrorDebugLoggingJobName, Namespace: sc.Namespace}, job)
		if err != nil {
			return reconcile.Result{}, err
		}
		if job.Status.Succeeded == 0 {
			// Job has not succeeded yet, so requeue a reconcile request after 10 sec to check again
			r.Log.Info("Waiting for enable-rbd-mirror-debug-logging job to complete")
			return reconcile.Result{RequeueAfter: 10 * time.Second}, nil
		}

		r.Log.Info("Successfully enabled rbd mirror debug logging")
		rbdMirrorDebugLoggingEnabled = true
	}
	// Mirroring is disabled but the debug logging is enabled
	if !sc.Spec.Mirroring.Enabled && rbdMirrorDebugLoggingEnabled {
		err := r.Client.Create(r.ctx, getRbdMirrorDebugLoggingJob(sc, disableRbdMirrorDebugLoggingJobName, disableRbdMirrorDebugLoggingCommands))
		if err != nil && !errors.IsAlreadyExists(err) {
			return reconcile.Result{}, err
		}
		// Check if the job has succeeded
		job := &batchv1.Job{}
		err = r.Client.Get(r.ctx, types.NamespacedName{Name: disableRbdMirrorDebugLoggingJobName, Namespace: sc.Namespace}, job)
		if err != nil {
			return reconcile.Result{}, err
		}
		if job.Status.Succeeded == 0 {
			// Job has not succeeded yet, so requeue a reconcile request after 10 sec to check again
			r.Log.Info("Waiting for disable-rbd-mirror-debug-logging job to complete")
			return reconcile.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
		}

		// Delete both the enable & disable jobs
		err = r.Client.Delete(r.ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      enableRbdMirrorDebugLoggingJobName,
				Namespace: sc.Namespace,
			}})
		if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
		err = r.Client.Delete(r.ctx, &batchv1.Job{
			ObjectMeta: metav1.ObjectMeta{
				Name:      disableRbdMirrorDebugLoggingJobName,
				Namespace: sc.Namespace,
			}})
		if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}

		r.Log.Info("Successfully disabled rbd mirror debug logging")
		rbdMirrorDebugLoggingEnabled = false
	}
	return reconcile.Result{}, nil
}

func getRbdMirrorDebugLoggingJob(sc *ocsv1.StorageCluster, jobName string, configCommands string) *batchv1.Job {
	rookImage := os.Getenv("ROOK_CEPH_IMAGE")
	debugJob := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      jobName,
			Namespace: sc.Namespace,
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: sc.APIVersion,
					Kind:       sc.Kind,
					Name:       sc.Name,
					UID:        sc.UID,
				},
			},
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					InitContainers: []corev1.Container{
						{
							Name:            "config-init",
							Image:           rookImage,
							Command:         []string{"/usr/local/bin/toolbox.sh"},
							Args:            []string{"--skip-watch"},
							ImagePullPolicy: corev1.PullIfNotPresent,
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
						},
					},
					Containers: []corev1.Container{
						{
							Name:  "script",
							Image: rookImage,
							VolumeMounts: []corev1.VolumeMount{
								{Name: "ceph-config", MountPath: "/etc/ceph", ReadOnly: true},
							},
							Command: []string{
								"bash",
								"-c",
								configCommands,
							},
							SecurityContext: &corev1.SecurityContext{
								AllowPrivilegeEscalation: ptr.To(false),
								Capabilities: &corev1.Capabilities{
									Drop: []corev1.Capability{
										"ALL",
									},
								},
							},
						},
					},
					Volumes: []corev1.Volume{
						{Name: "ceph-config", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}},
						{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{
								LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items:                []corev1.KeyToPath{{Key: "data", Path: "mon-endpoints"}},
							},
						},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					SecurityContext: &corev1.PodSecurityContext{
						RunAsNonRoot: ptr.To(true),
						SeccompProfile: &corev1.SeccompProfile{
							Type: corev1.SeccompProfileTypeRuntimeDefault,
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
				},
			},
		},
	}
	return debugJob
}
