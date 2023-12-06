package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephBlockPools struct{}

// ensures that peer cluster secret exists and adds it to CephBlockPool
func (r *StorageClusterReconciler) addPeerSecretsToCephBlockPool(initData *ocsv1.StorageCluster, poolName string, poolNamespace string) cephv1.MirroringPeerSpec {
	mirroringPeerSpec := cephv1.MirroringPeerSpec{}
	secretNames := []string{}

	if len(initData.Spec.Mirroring.PeerSecretNames) == 0 {
		err := fmt.Errorf("mirroring is enabled but peerSecretNames is not provided")
		r.Log.Error(err, "Unable to add cluster peer token to CephBlockPool.", "CephBlockPool", klog.KRef(poolNamespace, poolName))
		return mirroringPeerSpec
	}
	for _, secretName := range initData.Spec.Mirroring.PeerSecretNames {
		_, err := r.retrieveSecret(secretName, initData)
		if err != nil {
			r.Log.Error(err, "Peer cluster token could not be retrieved using secretname.", "CephBlockPool", klog.KRef(poolNamespace, poolName))
			return mirroringPeerSpec
		}
		secretNames = append(secretNames, secretName)
	}

	mirroringPeerSpec.SecretNames = secretNames
	return mirroringPeerSpec
}

// newCephBlockPoolInstances returns the cephBlockPool instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephBlockPoolInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephBlockPool, error) {
	var mirroringSpec cephv1.MirroringSpec
	poolName := generateNameForCephBlockPool(initData)
	poolNamespace := initData.Namespace

	if initData.Spec.Mirroring.Enabled {
		mirroringSpec.Enabled = true
		mirroringSpec.Mode = "image"
		mirroringPeerSpec := r.addPeerSecretsToCephBlockPool(initData, poolName, poolNamespace)
		mirroringSpec.Peers = &mirroringPeerSpec
	}

	ret := []*cephv1.CephBlockPool{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      poolName,
				Namespace: poolNamespace,
			},
			Spec: cephv1.NamedBlockPoolSpec{
				PoolSpec: cephv1.PoolSpec{
					DeviceClass:    generateDeviceClass(initData),
					FailureDomain:  getFailureDomain(initData),
					Replicated:     generateCephReplicatedSpec(initData, "data"),
					EnableRBDStats: true,
					Mirroring:      mirroringSpec,
				},
			},
		},
	}

	// Create Non-Resilient CephBlockPools if enabled
	if initData.Spec.ManagedResources.CephNonResilientPools.Enable {
		for _, failureDomainValue := range initData.Status.FailureDomainValues {
			ret = append(ret,
				&cephv1.CephBlockPool{
					ObjectMeta: metav1.ObjectMeta{
						Name:      generateNameForNonResilientCephBlockPool(initData, failureDomainValue),
						Namespace: initData.Namespace,
					},
					Spec: cephv1.NamedBlockPoolSpec{
						PoolSpec: cephv1.PoolSpec{
							DeviceClass:   failureDomainValue,
							FailureDomain: getFailureDomain(initData),
							Replicated: cephv1.ReplicatedSpec{
								Size:                   1,
								RequireSafeReplicaSize: false,
							},
							EnableRBDStats: true,
							Mirroring:      mirroringSpec,
						},
					},
				},
			)
		}

	}

	// create `.nfs` cephblockpool if NFS is enabled.
	if initData.Spec.NFS != nil && initData.Spec.NFS.Enable {
		ret = append(ret,
			&cephv1.CephBlockPool{
				ObjectMeta: metav1.ObjectMeta{
					Name:      generateNameForCephNFSBlockPool(initData),
					Namespace: initData.Namespace,
				},
				Spec: cephv1.NamedBlockPoolSpec{
					Name: ".nfs",
					PoolSpec: cephv1.PoolSpec{
						DeviceClass:    generateDeviceClass(initData),
						FailureDomain:  getFailureDomain(initData),
						Replicated:     generateCephReplicatedSpec(initData, "data"),
						EnableRBDStats: true,
					},
				},
			},
		)
	}

	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set controller reference for CephBlockPool.", "CephBlockPool", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}
	return ret, nil
}

// ensureCreated ensures that cephBlockPool resources exist in the desired
// state.
func (obj *ocsCephBlockPools) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}
	cephBlockPools, err := r.newCephBlockPoolInstances(instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	for _, cephBlockPool := range cephBlockPools {
		existing := cephv1.CephBlockPool{}
		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: cephBlockPool.Namespace}, &existing)

		switch {
		case err == nil:
			if reconcileStrategy == ReconcileStrategyInit {
				return reconcile.Result{}, nil
			}
			if existing.DeletionTimestamp != nil {
				r.Log.Info("Unable to restore CephBlockPool because it is marked for deletion.", "CephBlockPool", klog.KRef(existing.Namespace, existing.Name))
				return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			r.Log.Info("Restoring original CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			existing.ObjectMeta.OwnerReferences = cephBlockPool.ObjectMeta.OwnerReferences
			existing.Spec = cephBlockPool.Spec
			err = r.Client.Update(context.TODO(), &existing)
			if err != nil {
				r.Log.Error(err, "Failed to update CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
				return reconcile.Result{}, err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			err = r.Client.Create(context.TODO(), cephBlockPool)
			if err != nil {
				r.Log.Error(err, "Failed to create CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
				return reconcile.Result{}, err
			}
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephBlockPools owned by the StorageCluster
func (obj *ocsCephBlockPools) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	foundCephBlockPool := &cephv1.CephBlockPool{}
	cephBlockPools, err := r.newCephBlockPoolInstances(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, cephBlockPool := range cephBlockPools {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephBlockPool not found.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
				continue
			}
			return reconcile.Result{}, fmt.Errorf("uninstall: unable to retrieve CephBlockPool %v: %v", cephBlockPool.Name, err)
		}

		if cephBlockPool.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting CephBlockPool.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			err = r.Client.Delete(context.TODO(), foundCephBlockPool)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete CephBlockPool.", "CephBlockPool", klog.KRef(foundCephBlockPool.Namespace, foundCephBlockPool.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephBlockPool %v: %v", foundCephBlockPool.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: sc.Namespace}, foundCephBlockPool)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: CephBlockPool is deleted.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
				continue
			}
		}
		r.Log.Error(err, "Uninstall: Waiting for CephBlockPool to be deleted.", "CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephBlockPool %v to be deleted", cephBlockPool.Name)

	}
	return reconcile.Result{}, nil
}
