package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// newCephBlockPoolInstances returns the cephBlockPool instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephBlockPoolInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephBlockPool, error) {
	ret := []*cephv1.CephBlockPool{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephBlockPool(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.PoolSpec{
				FailureDomain: initData.Status.FailureDomain,
				Replicated: cephv1.ReplicatedSpec{
					Size:            3,
					TargetSizeRatio: .49,
				},
				EnableRBDStats: true,
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCephBlockPools ensures that cephBlockPool resources exist in the desired
// state.
func (r *StorageClusterReconciler) ensureCephBlockPools(instance *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	cephBlockPools, err := r.newCephBlockPoolInstances(instance)
	if err != nil {
		return err
	}
	for _, cephBlockPool := range cephBlockPools {
		existing := cephv1.CephBlockPool{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: cephBlockPool.Namespace}, &existing)

		switch {
		case err == nil:
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				r.Log.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info(fmt.Sprintf("Restoring original cephBlockPool %s", cephBlockPool.Name))
			existing.ObjectMeta.OwnerReferences = cephBlockPool.ObjectMeta.OwnerReferences
			cephBlockPool.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), cephBlockPool)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info(fmt.Sprintf("Creating cephBlockPool %s", cephBlockPool.Name))
			err = r.Client.Create(context.TODO(), cephBlockPool)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
