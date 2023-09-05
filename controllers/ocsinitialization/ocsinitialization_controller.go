package ocsinitialization

import (
	"context"
	"fmt"
	"reflect"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
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
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// watchNamespace is the namespace the operator is watching.
var watchNamespace string

const wrongNamespacedName = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."

// InitNamespacedName returns a NamespacedName for the singleton instance that
// should exist.
func InitNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      "ocsinit",
		Namespace: watchNamespace,
	}
}

// OCSInitializationReconciler reconciles a OCSInitialization object
// nolint:revive
type OCSInitializationReconciler struct {
	client.Client
	ctx            context.Context
	Log            logr.Logger
	Scheme         *runtime.Scheme
	SecurityClient secv1client.SecurityV1Interface
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
	r.ctx = ctx

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

	err = r.ensureRookCephOperatorConfigExists(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure rook-ceph-operator-config ConfigMap")
		return reconcile.Result{}, err
	}

	err = r.ensureOcsOperatorConfigExists(instance)
	if err != nil {
		r.Log.Error(err, "Failed to ensure ocs-operator-config ConfigMap")
		return reconcile.Result{}, err
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

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.OCSInitialization{}).
		Owns(&appsv1.Deployment{}).
		// Watcher for rook-ceph-operator-config cm
		Watches(
			&source.Kind{
				Type: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      util.RookCephOperatorConfigName,
						Namespace: watchNamespace,
					},
				},
			},
			handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: InitNamespacedName(),
				}}
			}),
		).
		// Watcher for ocs-operator-config cm
		Watches(
			&source.Kind{
				Type: &corev1.ConfigMap{
					ObjectMeta: metav1.ObjectMeta{
						Name:      util.OcsOperatorConfigName,
						Namespace: watchNamespace,
					},
				},
			},
			handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
				return []reconcile.Request{{
					NamespacedName: InitNamespacedName(),
				}}
			}),
		).
		Complete(r)
}

// ensureRookCephOperatorConfigExists ensures that the rook-ceph-operator-config cm exists
// This configmap is purely reserved for any user overrides to be applied.
// We don't reconcile it if it exists as it can reset any values the user has set
// The configmap is watched by the rook operator and values set here have higher precedence
// than the default values set in the rook operator pod env vars.
func (r *OCSInitializationReconciler) ensureRookCephOperatorConfigExists(initialData *ocsv1.OCSInitialization) error {
	rookCephOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.RookCephOperatorConfigName,
			Namespace: initialData.Namespace,
		},
	}
	err := r.Client.Create(r.ctx, rookCephOperatorConfig)
	if err != nil && !errors.IsAlreadyExists(err) {
		r.Log.Error(err, fmt.Sprintf("Failed to create %s configmap", util.RookCephOperatorConfigName))
		return err
	}
	return nil
}

// ensureOcsOperatorConfigExists ensures that the ocs-operator-config exists & if not create it with default values
// This configmap is reserved just for ocs operator use, primarily meant for passing values to rook-ceph-operator
// It is not meant to be modified by the user
// The values are set by the storagecluster controller at the start of the Reconcile
// The needed keys from the configmap are passed to rook-ceph operator pod as env variables.
// When any value in the configmap is updated, the rook-ceph-operator pod is restarted to pick up the new values.
func (r *OCSInitializationReconciler) ensureOcsOperatorConfigExists(initialData *ocsv1.OCSInitialization) error {
	// Default or placeholder data that is put during the creation of the configmap
	// The values are updated by the StorageCluster controller later if required
	ocsOperatorConfigData := map[string]string{
		util.ClusterNameKey:              util.GetClusterID(r.ctx, r.Client, &r.Log),
		util.EnableReadAffinityKey:       "true",
		util.CephFSKernelMountOptionsKey: "ms_mode=prefer-crc",
		util.EnableNFSKey:                "false",
	}
	ocsOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.OcsOperatorConfigName,
			Namespace: initialData.Namespace,
		},
		Data: ocsOperatorConfigData,
	}
	opResult, err := ctrl.CreateOrUpdate(r.ctx, r.Client, ocsOperatorConfig, func() error {
		// If the configmap is being controlled by a StorageCluster, nothing to do here
		if metav1.GetControllerOf(ocsOperatorConfig) != nil && metav1.GetControllerOf(ocsOperatorConfig).Kind == "StorageCluster" {
			return nil
		}
		// If the configmap is not controlled by a StorageCluster, keep it updated with the default values & set OCSInitialization as the controller
		if !reflect.DeepEqual(ocsOperatorConfig.Data, ocsOperatorConfigData) {
			r.Log.Info("Updating ocs-operator-config configmap")
			ocsOperatorConfig.Data = ocsOperatorConfigData
		}
		return ctrl.SetControllerReference(initialData, ocsOperatorConfig, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update ocs-operator-config configmap", "OperationResult", opResult)
		return err
	}
	// If configmap is created or updated, restart the rook-ceph-operator pod to pick up the new change
	if opResult == controllerutil.OperationResultCreated || opResult == controllerutil.OperationResultUpdated {
		r.Log.Info("ocs-operator-config configmap created/updated. Restarting rook-ceph-operator pod to pick up the new values")
		util.RestartPod(r.ctx, r.Client, &r.Log, "rook-ceph-operator", initialData.Namespace)
	}

	return nil
}
