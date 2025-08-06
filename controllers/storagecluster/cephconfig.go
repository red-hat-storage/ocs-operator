package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// TODO: Remove this file in the next release
type ocsCephConfig struct{}

const (
	rookOverrideConfigMapName = "rook-config-override"
)

var (
	rookOverrideCMIrrelevant bool
)

// ensureCreated checks for the override ConfigMap and deletes it if it has an owner reference to the StorageCluster CR.
func (obj *ocsCephConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	if rookOverrideCMIrrelevant {
		// If the ConfigMap has already been deleted or is absent or not owned by us, we can skip further checks.
		return reconcile.Result{}, nil
	}
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephConfig.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore || reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}
	rookConfigOverrideCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookOverrideConfigMapName,
			Namespace: sc.Namespace,
		},
	}
	err := r.Client.Get(context.TODO(), client.ObjectKeyFromObject(rookConfigOverrideCM), rookConfigOverrideCM)

	if err != nil {
		if errors.IsNotFound(err) {
			rookOverrideCMIrrelevant = true
			r.Log.Info("rook config override configMap not found, marking as irrelevant")
			return reconcile.Result{}, nil
		}
		return reconcile.Result{}, fmt.Errorf("failed to get rook config override configmap: %w", err)
	}

	// Delete the ConfigMap if it has the StorageCluster as owner
	for _, ownerRef := range rookConfigOverrideCM.OwnerReferences {
		if ownerRef.Kind == "StorageCluster" && ownerRef.Name == sc.Name && ownerRef.Controller != nil && *ownerRef.Controller {
			r.Log.Info("deleting rook config override configMap with StorageCluster owner reference")
			err = r.Client.Delete(context.Background(), rookConfigOverrideCM)
			if err != nil {
				r.Log.Error(err, "failed to delete rook config override configmap",
					"ConfigMap", klog.KRef(sc.Namespace, rookOverrideConfigMapName))
				return reconcile.Result{}, fmt.Errorf("failed to delete rook config override configmap: %w", err)
			}
			break
		}
	}
	rookOverrideCMIrrelevant = true
	r.Log.Info("rook config override configMap marked as irrelevant")
	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsCephConfig
func (obj *ocsCephConfig) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}
