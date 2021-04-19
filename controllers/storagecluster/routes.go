package storagecluster

import (
	"context"
	"fmt"

	routev1 "github.com/openshift/api/route/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsCephRGWRoutes struct{}

// ensureCreated ensures that CephObjectStore resources exist in the desired
// state.
func (obj *ocsCephRGWRoutes) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}
	avoid, err := r.PlatformsShouldAvoidObjectStore()
	if err != nil {
		return err
	}
	if avoid {
		r.Log.Info(fmt.Sprintf("not creating a Ceph RGW route because the platform is '%s'", r.platform.GetPlatform()))
		return nil
	}

	ocsCephRoutes, err := r.newCephRGWRoutes(instance)
	if err != nil {
		return err
	}
	err = r.createCephRGWRoutes(ocsCephRoutes, instance)
	if err != nil {
		r.Log.Error(err, "could not create Routes")
		return err
	}

	return nil
}

// ensureDeleted deletes the CephObjectStores owned by the StorageCluster
func (obj *ocsCephRGWRoutes) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	foundRoute := &routev1.Route{}
	routes, err := r.newCephRGWRoutes(sc)
	if err != nil {
		return err
	}

	for _, route := range routes {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: route.Name, Namespace: sc.Namespace}, foundRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: Route not found", "Route Name", route.Name)
				continue
			}
			return fmt.Errorf("Uninstall: Unable to retrieve route %v: %v", route.Name, err)
		}

		if route.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting route", "Route Name", route.Name)
			err = r.Client.Delete(context.TODO(), foundRoute)
			if err != nil {
				return fmt.Errorf("Uninstall: Failed to delete route %v: %v", route.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: route.Name, Namespace: sc.Namespace}, foundRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: Route is deleted", "Route Name", route.Name)
				continue
			}
		}
		return fmt.Errorf("Uninstall: Waiting for route %v to be deleted", route.Name)

	}
	return nil
}

// createCephObjectStore creates CephObjectStore in the desired state
func (r *StorageClusterReconciler) createCephRGWRoutes(routes []*routev1.Route, instance *ocsv1.StorageCluster) error {
	for _, route := range routes {
		existing := routev1.Route{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: route.Name, Namespace: route.Namespace}, &existing)
		switch {
		case err == nil:
			reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
			if reconcileStrategy == ReconcileStrategyInit {
				return nil
			}
			if existing.DeletionTimestamp != nil {
				err := fmt.Errorf("failed to restore route object %s because it is marked for deletion", existing.Name)
				r.Log.Info("route restore failed")
				return err
			}

			r.Log.Info(fmt.Sprintf("Restoring original route %s", route.Name))
			existing.ObjectMeta.OwnerReferences = route.ObjectMeta.OwnerReferences
			route.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), route)
			if err != nil {
				r.Log.Error(err, fmt.Sprintf("failed to update route Object: %s", route.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info(fmt.Sprintf("creating route %s", route.Name))
			err = r.Client.Create(context.TODO(), route)
			if err != nil {
				r.Log.Error(err, fmt.Sprintf("failed to create CephObjectStore object: %s", route.Name))
				return err
			}
		}
	}
	return nil
}

// newCephRGWRoutes returns the RGW route instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephRGWRoutes(initData *ocsv1.StorageCluster) ([]*routev1.Route, error) {
	// Use the same name as for the Ceph Object Store
	ret := []*routev1.Route{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: routev1.RouteSpec{
				To: routev1.RouteTargetReference{
					Kind: "Service",
					Name: generateNameForCephObjectStoreService(initData),
				},
			},
		},
	}

	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, fmt.Sprintf("Failed to set ControllerReference to %s", obj.Name))
			return nil, err
		}
	}
	return ret, nil
}

// generateNameForCephObjectStoreService is temporary - we should ideally get this name from rook
func generateNameForCephObjectStoreService(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-%s", "rook-ceph-rgw", generateNameForCephObjectStore(initData))
}
