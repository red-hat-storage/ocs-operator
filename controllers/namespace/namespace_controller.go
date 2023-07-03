package namespace

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/go-logr/logr"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// once is a pointer to the sync.Once type
var once sync.Once

// NamespaceReconciler reconciles the namespaces starting with openshift-*
// nolint:revive
type NamespaceReconciler struct {
	client.Client
	ctx    context.Context
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=core,resources=pods,verbs=list;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get;list;watch;update;patch

// Reconcile reads that state of the cluster for a Namespace object and makes changes based on the state read
// i.e,the annotations of the namespace
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *NamespaceReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Name", request.Name)
	r.ctx = ctx

	r.Log.Info("Reconciling Namespace.")

	var err error
	updated := false

	once.Do(func() {
		updated, err = r.annotateOpenshiftNamespaces()
	})

	// If it's not the first reconcile and the namespaces were not updated in the once.Do func call
	if !updated {
		ns := &corev1.Namespace{}
		err = r.Client.Get(ctx, request.NamespacedName, ns)
		if err != nil {
			// the namespace is probably deleted
			if errors.IsNotFound(err) {
				r.Log.Error(err, fmt.Sprintf("Namespace %s does not exist", request.Name))
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		// Annotate namespaces starting with openshift-*, but ignore namespace if
		// it has annotation "'skipReclaimspaceSchedule':'true'" set
		updated, err = r.annotateNamespaces([]corev1.Namespace{*ns})
		if err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to annotate namespace: %s", ns.Name))
			return reconcile.Result{}, err
		}
	}

	// Restart the csi-addons controller pod if any namespace was annotated
	if updated {
		err = r.restartCSIAddonsPod()
	}

	return reconcile.Result{}, err
}

// annotateOpenshiftNamespaces annotates all the namespaces starting with openshift-*
func (r *NamespaceReconciler) annotateOpenshiftNamespaces() (bool, error) {
	nsList := &corev1.NamespaceList{}
	err := r.Client.List(r.ctx, nsList)
	if err != nil {
		r.Log.Error(err, "Failed to list Namespaces")
		return false, err
	}

	openshiftNamespaces := []corev1.Namespace{}
	for _, ns := range nsList.Items {
		if strings.HasPrefix(ns.Name, "openshift-") {
			openshiftNamespaces = append(openshiftNamespaces, ns)
		}
	}

	return r.annotateNamespaces(openshiftNamespaces)
}

// annotateNamespaces annotate the provided namespace but, ignores if
// shouldSkipAnnotation() returns true
func (r *NamespaceReconciler) annotateNamespaces(opNs []corev1.Namespace) (bool, error) {
	updated := false
	for _, ns := range opNs {
		if shouldSkipAnnotation(ns) {
			continue
		}
		if ns.Annotations == nil {
			ns.Annotations = make(map[string]string)
		}
		ns.Annotations["reclaimspace.csiaddons.openshift.io/schedule"] = "@weekly"

		err := r.Client.Update(r.ctx, &ns)
		if err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to annotate namespace: %s", ns.Name))
			return false, err
		}
		r.Log.Info(fmt.Sprintf("Successfully annotated namespace: %s", ns.Name))
		updated = true
	}
	return updated, nil
}

// shouldSkipAnnotation checks if we should skip adding new annotations to the namespace
func shouldSkipAnnotation(ns corev1.Namespace) bool {
	if !ns.GetDeletionTimestamp().IsZero() || ns.Annotations["skipReclaimspaceSchedule"] == "true" ||
		ns.Annotations["reclaimspace.csiaddons.openshift.io/schedule"] != "" {
		return true
	}
	return false
}

// restartCSIAddonsPod restarts the CSIAddons pod by deleting all of its instances.
func (r *NamespaceReconciler) restartCSIAddonsPod() error {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(os.Getenv(util.OperatorNamespaceEnvVar)),
		client.MatchingLabels{"app.kubernetes.io/name": "csi-addons"},
	}
	err := r.Client.List(r.ctx, podList, listOpts...)
	if err != nil {
		r.Log.Error(err, "Failed to get list of pods with label 'csi-addons'")
		return err
	}

	// If no csi-addons pod is present
	if len(podList.Items) == 0 {
		r.Log.Error(err, "No csi-addons pod found")
		return nil
	}

	for _, csiAddonsPod := range podList.Items {
		err = r.Client.Delete(r.ctx, &csiAddonsPod, client.GracePeriodSeconds(0))
		if err != nil {
			r.Log.Error(err, "Failed to delete csi-addons pod")
			return err
		}
	}

	return nil
}

// SetupWithManager sets up a controller with a manager
func (r *NamespaceReconciler) SetupWithManager(mgr ctrl.Manager) error {

	predicateFunc := func(obj runtime.Object) bool {
		instance, ok := obj.(*corev1.Namespace)
		if !ok {
			return false
		}

		// ignore if the namespace's name doesn't start with openshift-*
		if !strings.HasPrefix(instance.Name, "openshift-") {
			return false
		}

		// If the namespace has already been annotated, or skip annotation is set,
		// or the namespace is marked for deletion
		if shouldSkipAnnotation(*instance) {
			return false
		}

		return true
	}

	nameSpacePredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return predicateFunc(e.Object)
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return predicateFunc(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Ignore delete events
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Namespace{},
			builder.WithPredicates(nameSpacePredicate)).
		Complete(r)
}
