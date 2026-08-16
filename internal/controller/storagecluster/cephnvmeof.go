package storagecluster

import (
	"cmp"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	defaultNVMeOFGatewayGroup    = "group-a"
	nvmeofMetadataPoolName       = "builtin-nvmeof"
	nvmeofPoolReadyRequeuePeriod = 5 * time.Second
)

type ocsCephNVMeOF struct{}

// ensureCreated ensures that NVMe-oF backend resources exist.
// The CephBlockPool is reconciled in cephblockpools.go (consistent with NFS pattern).
// CSI Driver and StorageClass are managed via the provider RPC to client-operator.
func (obj *ocsCephNVMeOF) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	if sc.Spec.NVMeOF == nil || !sc.Spec.NVMeOF.Enable {
		return obj.ensureDeleted(r, sc)
	}
	if ReconcileStrategy(sc.Spec.NVMeOF.ReconcileStrategy) == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	ready, err := obj.isNVMeOFMetadataPoolReady(r, sc)
	if err != nil {
		return reconcile.Result{}, err
	}
	if !ready {
		r.Log.Info("Waiting for .nvmeof metadata pool to be ready before creating CephNVMeOFGateway.")
		return reconcile.Result{RequeueAfter: nvmeofPoolReadyRequeuePeriod}, nil
	}

	if res, err := obj.ensureNVMeOFGateway(r, sc); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

// isNVMeOFMetadataPoolReady checks whether the .nvmeof metadata CephBlockPool is in Ready phase.
// This prevents creating the CephNVMeOFGateway before the pool exists in RADOS,
// avoiding transient CrashLoopBackOff on gateway pods during initial deployment.
func (obj *ocsCephNVMeOF) isNVMeOFMetadataPoolReady(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (bool, error) {
	pool := &cephv1.CephBlockPool{}
	pool.Name = nvmeofMetadataPoolName
	pool.Namespace = sc.Namespace

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(pool), pool); err != nil {
		if errors.IsNotFound(err) {
			return false, nil
		}
		return false, err
	}

	return pool.Status != nil && pool.Status.Phase == cephv1.ConditionReady, nil
}

// ensureDeleted deletes NVMe-oF backend resources owned by the StorageCluster.
// The CephBlockPool deletion is handled in cephblockpools.go.
// CSI Driver and StorageClass lifecycle is managed by client-operator via RPC.
func (obj *ocsCephNVMeOF) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	gwName := util.GenerateNameForCephNVMeOFGateway(sc)
	gateway := &cephv1.CephNVMeOFGateway{}
	gateway.Name = gwName
	gateway.Namespace = sc.Namespace

	if err := r.Get(r.ctx, client.ObjectKeyFromObject(gateway), gateway); err != nil {
		if errors.IsNotFound(err) {
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, err
	}

	r.Log.Info("Uninstall: Deleting CephNVMeOFGateway.", "CephNVMeOFGateway", client.ObjectKeyFromObject(gateway))
	if err := r.Delete(r.ctx, gateway); err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureNVMeOFGateway creates or updates the CephNVMeOFGateway.
func (obj *ocsCephNVMeOF) ensureNVMeOFGateway(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	gateway := &cephv1.CephNVMeOFGateway{}
	gateway.Name = util.GenerateNameForCephNVMeOFGateway(sc)
	gateway.Namespace = sc.Namespace

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, gateway, func() error {
		gateway.Spec = cephv1.NVMeOFGatewaySpec{
			Group:       cmp.Or(sc.Spec.NVMeOF.GatewayGroup, defaultNVMeOFGatewayGroup),
			Instances:   sc.Spec.NVMeOF.GatewayInstances,
			HostNetwork: ptr.To(false),
		}
		return controllerutil.SetControllerReference(sc, gateway, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update CephNVMeOFGateway.", "CephNVMeOFGateway", client.ObjectKeyFromObject(gateway))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}
