package ocsinitialization

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// watchNamespace is the namespace the operator is watching.
var watchNamespace string

const wrongNamespacedName = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."

const (
	rookCephToolDeploymentName = "rook-ceph-tools"
	// This name is predefined by Rook
	rookCephOperatorConfigName = "rook-ceph-operator-config"
)

// InitNamespacedName returns a NamespacedName for the singleton instance that
// should exist.
func InitNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      "ocsinit",
		Namespace: watchNamespace,
	}
}

// OCSInitializationReconciler reconciles a OCSInitialization object
//nolint
type OCSInitializationReconciler struct {
	client.Client
	Log            logr.Logger
	Scheme         *runtime.Scheme
	SecurityClient secv1client.SecurityV1Interface
	RookImage      string
}

func newToolsDeployment(namespace string, rookImage string) *appsv1.Deployment {

	name := rookCephToolDeploymentName
	var replicaOne int32 = 1

	// privileged needs to be true due to permission issues
	privilegedContainer := true
	runAsNonRoot := true
	var runAsUser, runAsGroup int64 = 2016, 2016
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
							Command: []string{"/bin/bash"},
							Args: []string{
								"-m",
								"-c",
								"/usr/local/bin/toolbox.sh",
							},
							TTY: true,
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
								Privileged:   &privilegedContainer,
								RunAsNonRoot: &runAsNonRoot,
								RunAsUser:    &runAsUser,
								RunAsGroup:   &runAsGroup,
							},
							VolumeMounts: []corev1.VolumeMount{
								{Name: "ceph-config", MountPath: "/etc/ceph"},
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
						{Name: "ceph-config", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/etc/ceph"}}},
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

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,verbs=get;create;update
// +kubebuilder:rbac:groups=security.openshift.io,resourceNames=privileged,resources=securitycontextconstraints,verbs=get;create;update

// Reconcile reads that state of the cluster for a OCSInitialization object and makes changes based on the state read
// and what is in the OCSInitialization.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *OCSInitializationReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	r.Log.Info("Reconciling OCSInitialization.", "OCSInitialization", klog.KRef(request.Namespace, request.Name))

	initNamespacedName := InitNamespacedName()
	instance := &ocsv1.OCSInitialization{}
	if initNamespacedName.Name != request.Name || initNamespacedName.Namespace != request.Namespace {
		// Ignoring this resource because it has the wrong name or namespace
		r.Log.Info(wrongNamespacedName)
		err := r.Client.Get(ctx, request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		instance.Status.Phase = util.PhaseIgnored
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to update ignored OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
		}
		return reconcile.Result{}, err
	}

	// Fetch the OCSInitialization instance
	err := r.Client.Get(ctx, request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Recreating since we depend on this to exist. A user may delete it to
			// induce a reset of all initial data.
			r.Log.Info("Recreating OCSInitialization resource.")
			return reconcile.Result{}, r.Client.Create(ctx, &ocsv1.OCSInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      initNamespacedName.Name,
					Namespace: initNamespacedName.Namespace,
				},
			})
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing OCSInitialization resource"
		util.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseProgressing
		err = r.Client.Status().Update(ctx, instance)
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status of OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
			return reconcile.Result{}, err
		}
	}

	err = r.ensureSCCs(instance)
	if err != nil {
		reason := ocsv1.ReconcileFailed
		message := fmt.Sprintf("Error while reconciling: %v", err)
		util.SetErrorCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseError
		// don't want to overwrite the actual reconcile failure
		uErr := r.Client.Status().Update(ctx, instance)
		if uErr != nil {
			r.Log.Error(uErr, "Failed to update conditions of OCSInitialization resource.", "OCSInitialization", klog.KRef(instance.Namespace, instance.Name))
		}
		return reconcile.Result{}, err
	}
	instance.Status.SCCsCreated = true

	err = r.Client.Status().Update(ctx, instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.ensureToolsDeployment(instance)
	if err != nil {
		r.Log.Error(err, "Failed to process ceph tools deployment.", "CephToolDeployment", klog.KRef(instance.Namespace, rookCephToolDeploymentName))
		return reconcile.Result{}, err
	}

	if !instance.Status.RookCephOperatorConfigCreated {
		// if true, no need to ensure presence of ConfigMap
		// if false, ensure ConfigMap and update the status
		err = r.ensureRookCephOperatorConfig(instance)
		if err != nil {
			r.Log.Error(err, "Failed to process ConfigMap.", "ConfigMap", klog.KRef(instance.Namespace, rookCephOperatorConfigName))
			return reconcile.Result{}, err
		}
		instance.Status.RookCephOperatorConfigCreated = true
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	util.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = util.PhaseReady
	err = r.Client.Status().Update(ctx, instance)

	return reconcile.Result{}, err
}

// SetupWithManager sets up a controller with a manager
func (r *OCSInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ns, err := util.GetWatchNamespace()
	if err != nil {
		return err
	}
	watchNamespace = ns

	rookImage := os.Getenv("ROOK_CEPH_IMAGE")
	if rookImage == "" {
		return fmt.Errorf("No ROOK_CEPH_IMAGE environment variable set")
	}
	r.RookImage = rookImage

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.OCSInitialization{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// returns a ConfigMap with default settings for rook-ceph operator
func newRookCephOperatorConfig(namespace string) *corev1.ConfigMap {
	var defaultCSIToleration = `
- key: ` + defaults.NodeTolerationKey + `
  operator: Equal
  value: "true"
  effect: NoSchedule`

	config := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookCephOperatorConfigName,
			Namespace: namespace,
		},
	}
	data := make(map[string]string)
	data["CSI_PROVISIONER_TOLERATIONS"] = defaultCSIToleration
	data["CSI_PLUGIN_TOLERATIONS"] = defaultCSIToleration
	data["CSI_LOG_LEVEL"] = "5"
	data["CSI_ENABLE_CSIADDONS"] = "true"
	config.Data = data

	return config
}

func (r *OCSInitializationReconciler) ensureRookCephOperatorConfig(initialData *ocsv1.OCSInitialization) error {
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: initialData.Namespace}, rookCephOperatorConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// If it does not exist, create a ConfigMap with default settings
			return r.Client.Create(context.TODO(), newRookCephOperatorConfig(initialData.Namespace))
		}
		return err
	}
	// If it already exists, do not update. It is up to the user to
	// update the ConfigMap as they see fit. Changes will be picked
	// up by rook operator and reconciled. We do not want to reconcile
	// this ConfigMap. If we do, user changes will be reset to defaults.
	return nil
}
