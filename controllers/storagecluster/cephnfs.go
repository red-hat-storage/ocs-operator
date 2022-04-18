package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephNFS struct{}

// newCephNFSInstance returns the cephNFS instance that should be created
// on first run.
func (r *StorageClusterReconciler) newCephNFSInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephNFS, error) {
	ret := []*cephv1.CephNFS{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephNFS(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.NFSGaneshaSpec{
				Server: cephv1.GaneshaServerSpec{
					Active:    1,
					Placement: getPlacement(initData, "nfs"),
					Resources: defaults.GetDaemonResources("nfs", initData.Spec.Resources),
					// set high PriorityClassName for the NFS pods, since this will block io for
					// pods using NFS volumes.
					PriorityClassName: openshiftUserCritical,
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set Controller Reference for CephNFS.", "CephNFS", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}
	return ret, nil
}

// ensureCreated ensures that cephNFS resource exist in the desired state.
func (obj *ocsCephNFS) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if instance.Spec.NFS == nil || !instance.Spec.NFS.Enable {
		return reconcile.Result{}, nil
	}

	cephNFSes, err := r.newCephNFSInstances(instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, cephNFS := range cephNFSes {
		existingCephNFS := cephv1.CephNFS{}
		ctxTODO := context.TODO()
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: cephNFS.Name, Namespace: cephNFS.Namespace}, &existingCephNFS)
		switch {
		case err == nil:
			if existingCephNFS.DeletionTimestamp != nil {
				r.Log.Info("Unable to restore CephNFS because it is marked for deletion.", "CephNFS", klog.KRef(existingCephNFS.Namespace, existingCephNFS.Name))
				return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %q because it is marked for deletion", existingCephNFS.Name)
			}

			r.Log.Info("Restoring original CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
			existingCephNFS.ObjectMeta.OwnerReferences = cephNFS.ObjectMeta.OwnerReferences
			existingCephNFS.Spec = cephNFS.Spec
			err = r.Client.Update(ctxTODO, &existingCephNFS)
			if err != nil {
				r.Log.Error(err, "Unable to update CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
				return reconcile.Result{}, err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
			err = r.Client.Create(ctxTODO, cephNFS)
			if err != nil {
				r.Log.Error(err, "Unable to create CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
				return reconcile.Result{}, err
			}
		default:
			r.Log.Error(err, fmt.Sprintf("Unable to retrieve CephNFS %q.", cephNFS.Name), "CephNFS",
				klog.KRef(cephNFS.Namespace, cephNFS.Name))
			return reconcile.Result{}, fmt.Errorf("Unable to retrieve CephNFS %q: %w", cephNFS.Name, err)
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephNFS resource owned by the StorageCluster
func (obj *ocsCephNFS) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	ctxTODO := context.TODO()
	foundCephNFS := &cephv1.CephNFS{}
	cephNFSes, err := r.newCephNFSInstances(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, cephNFS := range cephNFSes {
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: cephNFS.Name, Namespace: sc.Namespace}, foundCephNFS)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephNFS not found.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve CephNFS %q: %w", cephNFS.Name, err)
		}

		if cephNFS.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephNFS.", "CephNFS", klog.KRef(foundCephNFS.Namespace, foundCephNFS.Name))
			err = r.Client.Delete(ctxTODO, foundCephNFS)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephNFS.", "CephNFS", klog.KRef(foundCephNFS.Namespace, foundCephNFS.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephNFS %q: %w", foundCephNFS.Name, err)
			}
		}

		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: cephNFS.Name, Namespace: sc.Namespace}, foundCephNFS)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephNFS is deleted.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve CephNFS.", "CephNFS", klog.KRef(cephNFS.Namespace, cephNFS.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve CephNFS %q: %w", cephNFS.Name, err)
		}

		err = fmt.Errorf("CephNFS %q still exists", cephNFS.Name)
		r.Log.Error(err, "Uninstall: Waiting for CephNFS to be deleted.", "CephNFS",
			klog.KRef(cephNFS.Namespace, cephNFS.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephNFS %q to be deleted: %w", cephNFS.Name, err)
	}

	return reconcile.Result{}, nil
}
