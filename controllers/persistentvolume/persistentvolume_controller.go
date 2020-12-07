package persistentvolume

import (
	"strings"

	"github.com/go-logr/logr"
	"github.com/openshift/ocs-operator/controllers/util"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	csiExpansionSecretName      = "csi.storage.k8s.io/controller-expand-secret-name"
	csiExpansionSecretNamespace = "csi.storage.k8s.io/controller-expand-secret-namespace"
	csiRBDDriverSuffix          = ".rbd.csi.ceph.com"
	csiCephFSDriverSuffix       = ".cephfs.csi.ceph.com"
)

var log = logf.Log.WithName("controller_persistentvolume")

// Add creates a new PersistentVolume Controller and adds it to the Manager.
// The Manager will set fields on the Controller and Start it when the Manager
// is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	r := &ReconcilePersistentVolume{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		reqLogger: log,
	}

	return r
}

// reconcilePV determines whether we want to reconcile a given PV
func reconcilePV(obj runtime.Object) bool {
	pv, ok := obj.(*corev1.PersistentVolume)
	if !ok {
		return false
	}

	drivers := []string{
		csiRBDDriverSuffix,
		csiCephFSDriverSuffix,
	}

	for _, driver := range drivers {
		if pv.Spec.CSI != nil && strings.HasSuffix(pv.Spec.CSI.Driver, driver) {
			if pv.Spec.StorageClassName != "" {
				secretRef := pv.Spec.CSI.ControllerExpandSecretRef
				if secretRef == nil || secretRef.Name == "" {
					return true
				}
			}
			return false
		}
	}

	return false
}

var pvPredicate = predicate.Funcs{
	CreateFunc: func(e event.CreateEvent) bool {
		return reconcilePV(e.Object)
	},
	DeleteFunc: func(e event.DeleteEvent) bool {
		return false
	},
	UpdateFunc: func(e event.UpdateEvent) bool {
		return reconcilePV(e.ObjectNew)
	},
	GenericFunc: func(e event.GenericEvent) bool {
		return reconcilePV(e.Object)
	},
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("persistentvolume-controller", mgr, controller.Options{MaxConcurrentReconciles: 1, Reconciler: r})
	if err != nil {
		return err
	}

	// Compose a predicate that is an OR of the specified predicates
	predicate := util.ComposePredicates(
		predicate.ResourceVersionChangedPredicate{},
		util.MetadataChangedPredicate{},
	)

	// Watch for changes to PersistentVolumes
	err = c.Watch(&source.Kind{Type: &corev1.PersistentVolume{}}, &handler.EnqueueRequestForObject{}, predicate, pvPredicate)
	if err != nil {
		return err
	}

	return nil
}

// ReconcilePersistentVolume reconciles a PersistentVolume object
type ReconcilePersistentVolume struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client    client.Client
	scheme    *runtime.Scheme
	reqLogger logr.Logger
}

var _ reconcile.Reconciler = &ReconcilePersistentVolume{}
