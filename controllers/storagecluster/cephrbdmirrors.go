package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephRbdMirrors struct{}

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
				Name:      util.GenerateNameForCephRbdMirror(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.RBDMirroringSpec{
				Count: func() int {
					if initData.Spec.ManagedResources.CephRBDMirror.DaemonCount == 0 {
						return 1
					}
					return initData.Spec.ManagedResources.CephRBDMirror.DaemonCount
				}(),
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

	if instance.Spec.Mirroring == nil || !instance.Spec.Mirroring.Enabled {
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
