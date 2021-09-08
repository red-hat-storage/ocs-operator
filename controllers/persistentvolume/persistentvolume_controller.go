package persistentvolume

import (
	"strings"

	"github.com/red-hat-storage/ocs-operator/controllers/util"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const (
	csiExpansionSecretName      = "csi.storage.k8s.io/controller-expand-secret-name"
	csiExpansionSecretNamespace = "csi.storage.k8s.io/controller-expand-secret-namespace"
	csiRBDDriverSuffix          = ".rbd.csi.ceph.com"
	csiCephFSDriverSuffix       = ".cephfs.csi.ceph.com"
)

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

// PersistentVolumeReconciler reconciles a PersistentVolume object
//nolint
type PersistentVolumeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

// SetupWithManager sets up a controller with a manager
func (r *PersistentVolumeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	// Compose a predicate that is an OR of the specified predicates
	predicate := util.ComposePredicates(
		predicate.ResourceVersionChangedPredicate{},
		util.MetadataChangedPredicate{},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.PersistentVolume{}, builder.WithPredicates(predicate, pvPredicate)).
		Complete(r)
}
