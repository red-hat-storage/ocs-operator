package ocsinitialization

import (
	"context"
	"fmt"
	"os"
	"reflect"

	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	"github.com/openshift/ocs-operator/pkg/controller/util"

	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_ocsinitialization")

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

// Add creates a new OCSInitialization Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	rookImage := os.Getenv("ROOK_CEPH_IMAGE")
	if rookImage == "" {
		panic(fmt.Errorf("No ROOK_CEPH_IMAGE environment variable set"))
	}

	return &ReconcileOCSInitialization{
		client:    mgr.GetClient(),
		secClient: secv1client.NewForConfigOrDie(mgr.GetConfig()),
		scheme:    mgr.GetScheme(),
		rookImage: rookImage,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// set the watchNamespace so we know where to create the OCSInitialization resource
	ns, err := util.GetWatchNamespace()
	if err != nil {
		return err
	}
	watchNamespace = ns

	// Create a new controller
	c, err := controller.New("ocsinitialization-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &appsv1.Deployment{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &ocsv1.StorageCluster{},
	})

	if err != nil {
		return err
	}

	// Watch for changes to primary resource OCSInitialization
	return c.Watch(&source.Kind{Type: &ocsv1.OCSInitialization{}}, &handler.EnqueueRequestForObject{})
}

// blank assignment to verify that ReconcileOCSInitialization implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileOCSInitialization{}

// ReconcileOCSInitialization reconciles a OCSInitialization object
type ReconcileOCSInitialization struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	secClient secv1client.SecurityV1Interface
	scheme    *runtime.Scheme
	rookImage string
}

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
						corev1.Container{
							Name:    name,
							Image:   rookImage,
							Command: []string{"/tini"},
							Args:    []string{"-g", "--", "/usr/local/bin/toolbox.sh"},
							Env: []corev1.EnvVar{
								corev1.EnvVar{
									Name: "ROOK_CEPH_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
											Key:                  "ceph-username",
										},
									},
								},
								corev1.EnvVar{
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
								corev1.VolumeMount{Name: "dev", MountPath: "/dev"},
								corev1.VolumeMount{Name: "sysbus", MountPath: "/sys/bus"},
								corev1.VolumeMount{Name: "libmodules", MountPath: "/lib/modules"},
								corev1.VolumeMount{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
							},
						},
					},
					Tolerations: []corev1.Toleration{
						corev1.Toleration{
							Key:      defaults.NodeTolerationKey,
							Operator: corev1.TolerationOpEqual,
							Value:    "true",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					// if hostNetwork: false, the "rbd map" command hangs, see https://github.com/rook/rook/issues/2021
					HostNetwork: true,
					Volumes: []corev1.Volume{
						corev1.Volume{Name: "dev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev"}}},
						corev1.Volume{Name: "sysbus", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys/bus"}}},
						corev1.Volume{Name: "libmodules", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/lib/modules"}}},
						corev1.Volume{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
							ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
								Items: []corev1.KeyToPath{
									corev1.KeyToPath{Key: "data", Path: "mon-endpoints"},
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

func (r *ReconcileOCSInitialization) ensureToolsDeployment(initialData *ocsv1.OCSInitialization) error {

	var isFound bool
	namespace := initialData.Namespace

	toolsDeployment := newToolsDeployment(namespace, r.rookImage)
	foundToolsDeployment := &appsv1.Deployment{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: rookCephToolDeploymentName, Namespace: namespace}, foundToolsDeployment)

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
			return r.client.Create(context.TODO(), toolsDeployment)
		} else if !reflect.DeepEqual(foundToolsDeployment.Spec, toolsDeployment.Spec) {

			updateDeployment := foundToolsDeployment.DeepCopy()
			updateDeployment.Spec = *toolsDeployment.Spec.DeepCopy()

			return r.client.Update(context.TODO(), updateDeployment)
		}
	} else if isFound {
		// delete if ceph tools exists and is disabled
		return r.client.Delete(context.TODO(), foundToolsDeployment)
	}

	return nil
}

// Reconcile reads that state of the cluster for a OCSInitialization object and makes changes based on the state read
// and what is in the OCSInitialization.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileOCSInitialization) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling OCSInitialization")

	initNamespacedName := InitNamespacedName()
	instance := &ocsv1.OCSInitialization{}
	if initNamespacedName.Name != request.Name || initNamespacedName.Namespace != request.Namespace {
		// Ignoring this resource because it has the wrong name or namespace
		reqLogger.Info(wrongNamespacedName)
		err := r.client.Get(context.TODO(), request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		instance.Status.Phase = statusutil.PhaseIgnored
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update ignored resource")
		}
		return reconcile.Result{}, err
	}

	// Fetch the OCSInitialization instance
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Recreating since we depend on this to exist. A user may delete it to
			// induce a reset of all initial data.
			reqLogger.Info("recreating OCSInitialization resource")
			return reconcile.Result{}, r.client.Create(context.TODO(), &ocsv1.OCSInitialization{
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
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = statusutil.PhaseProgressing
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "Failed to add conditions to status")
			return reconcile.Result{}, err
		}
	}

	err = r.ensureSCCs(instance, reqLogger)
	if err != nil {
		reason := ocsv1.ReconcileFailed
		message := fmt.Sprintf("Error while reconciling: %v", err)
		statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = statusutil.PhaseError
		// don't want to overwrite the actual reconcile failure
		uErr := r.client.Status().Update(context.TODO(), instance)
		if uErr != nil {
			reqLogger.Error(uErr, "Failed to update conditions")
		}
		return reconcile.Result{}, err
	}
	instance.Status.SCCsCreated = true

	err = r.client.Status().Update(context.TODO(), instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.ensureToolsDeployment(instance)
	if err != nil {
		reqLogger.Error(err, "Failed to process ceph tools deployment")
		return reconcile.Result{}, err
	}

	if !instance.Status.RookCephOperatorConfigCreated {
		// if true, no need to ensure presence of ConfigMap
		// if false, ensure ConfigMap and update the status
		err = r.ensureRookCephOperatorConfig(instance)
		if err != nil {
			reqLogger.Error(err, "Failed to process %s Configmap", rookCephOperatorConfigName)
			return reconcile.Result{}, err
		}
		instance.Status.RookCephOperatorConfigCreated = true
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = statusutil.PhaseReady
	err = r.client.Status().Update(context.TODO(), instance)

	return reconcile.Result{}, err
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
	config.Data = data

	return config
}

func (r *ReconcileOCSInitialization) ensureRookCephOperatorConfig(initialData *ocsv1.OCSInitialization) error {
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: initialData.Namespace}, rookCephOperatorConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// If it does not exist, create a ConfigMap with default settings
			return r.client.Create(context.TODO(), newRookCephOperatorConfig(initialData.Namespace))
		}
		return err
	}
	// If it already exists, do not update. It is up to the user to
	// update the ConfigMap as they see fit. Changes will be picked
	// up by rook operator and reconciled. We do not want to reconcile
	// this ConfigMap. If we do, user changes will be reset to defaults.
	return nil
}
