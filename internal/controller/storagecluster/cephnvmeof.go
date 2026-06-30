package storagecluster

import (
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/util"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	defaultNVMeOFGatewayGroup     = "group-a"
	defaultNVMeOFGatewayInstances = 2
)

type ocsCephNVMeOF struct{}

// ensureCreated ensures that NVMe-oF backend resources exist.
// The CephBlockPool is reconciled in cephblockpools.go (consistent with NFS pattern).
// CSI Driver and StorageClass are managed via the provider RPC to client-operator.
func (obj *ocsCephNVMeOF) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if instance.Spec.NVMeOF == nil {
		return reconcile.Result{}, nil
	}
	if ReconcileStrategy(instance.Spec.NVMeOF.ReconcileStrategy) == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	if res, err := obj.ensureNVMeOFGateway(r, instance); err != nil || !res.IsZero() {
		return res, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes NVMe-oF backend resources owned by the StorageCluster.
// The CephBlockPool deletion is handled in cephblockpools.go.
// CSI Driver and StorageClass lifecycle is managed by client-operator via RPC.
func (obj *ocsCephNVMeOF) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	if sc.Spec.NVMeOF == nil {
		return reconcile.Result{}, nil
	}

	// Delete CephNVMeOFGateway
	gwName := util.GenerateNameForCephNVMeOFGateway(sc)
	gateway := &cephv1.CephNVMeOFGateway{}
	if err := r.Get(r.ctx, types.NamespacedName{Name: gwName, Namespace: sc.Namespace}, gateway); err == nil {
		r.Log.Info("Uninstall: Deleting CephNVMeOFGateway.", "CephNVMeOFGateway", klog.KRef(sc.Namespace, gwName))
		if err := r.Delete(r.ctx, gateway); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, err
		}
	} else if !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureNVMeOFGateway creates or updates the CephNVMeOFGateway.
func (obj *ocsCephNVMeOF) ensureNVMeOFGateway(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	gwName := util.GenerateNameForCephNVMeOFGateway(sc)
	gateway := &cephv1.CephNVMeOFGateway{
		ObjectMeta: metav1.ObjectMeta{
			Name:      gwName,
			Namespace: sc.Namespace,
		},
	}

	group := sc.Spec.NVMeOF.GatewayGroup
	if group == "" {
		group = defaultNVMeOFGatewayGroup
	}
	instances := sc.Spec.NVMeOF.GatewayInstances
	if instances == 0 {
		instances = defaultNVMeOFGatewayInstances
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, gateway, func() error {
		gateway.Spec = cephv1.NVMeOFGatewaySpec{
			Pool:        util.GenerateNameForNVMeOFBlockPool(sc),
			Group:       group,
			Instances:   instances,
			HostNetwork: ptr.To(false),
		}
		return controllerutil.SetControllerReference(sc, gateway, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create/update CephNVMeOFGateway.", "CephNVMeOFGateway", klog.KRef(sc.Namespace, gwName))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

