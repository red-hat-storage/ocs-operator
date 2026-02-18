package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	routev1 "github.com/openshift/api/route/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephRGWRoutes struct{}

// ensureCreated ensures that CephObjectStore resources exist in the desired
// state.
func (obj *ocsCephRGWRoutes) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if instance.Spec.ManagedResources.CephObjectStores.DisableRoute {
		return reconcile.Result{}, nil
	}
	reconcileStrategy := ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	skip, err := platform.PlatformsShouldSkipObjectStore()
	if err != nil {
		return reconcile.Result{}, err
	}
	if skip {
		platformType, err := platform.GetPlatformType()
		if err != nil {
			return reconcile.Result{}, err
		}
		r.Log.Info("Platform is set to skip Ceph RGW Route. Not creating a Ceph RGW Route.", "platform", platformType)
		return reconcile.Result{}, nil
	}

	ocsCephRoutes, err := r.newCephRGWRoutes(instance)
	if err != nil {
		return reconcile.Result{}, err
	}
	err = r.createCephRGWRoutes(ocsCephRoutes, instance)
	if err != nil {
		r.Log.Error(err, "Could not create Ceph RGW Routes.")
		return reconcile.Result{}, err
	}

	// delete the http RGW route
	if instance.Spec.ManagedResources.CephObjectStores.DisableHttp {
		r.Log.Info("deleting any existing RGW http routes")
		err := r.deleteHttpRoute(instance)
		if err != nil {
			r.Log.Error(err, "failed to delete http Ceph RGW route.")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephObjectStores owned by the StorageCluster
func (obj *ocsCephRGWRoutes) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	if sc.Spec.ManagedResources.CephObjectStores.DisableRoute {
		return reconcile.Result{}, nil
	}
	foundRoute := &routev1.Route{}
	routes, err := r.newCephRGWRoutes(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, route := range routes {
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: route.Name, Namespace: sc.Namespace}, foundRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: Ceph RGW Route not found.", "CephRGWRoute", klog.KRef(sc.Namespace, route.Name))
				continue
			}
			return reconcile.Result{}, fmt.Errorf("Uninstall: Unable to retrieve route %v: %v", route.Name, err)
		}

		if route.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting Ceph RGW Route.", "CephRGWRoute", klog.KRef(sc.Namespace, route.Name))
			err = r.Client.Delete(context.TODO(), foundRoute)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete Ceph RGW Route.", "CephRGWRoute", klog.KRef(sc.Namespace, route.Name))
				return reconcile.Result{}, fmt.Errorf("Uninstall: Failed to delete Route %v: %v", route.Name, err)
			}
		}

		err = r.Client.Get(context.TODO(), types.NamespacedName{Name: route.Name, Namespace: sc.Namespace}, foundRoute)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: Ceph RGW Route is deleted.", "CephRGWRoute", klog.KRef(sc.Namespace, route.Name))
				continue
			}
		}
		return reconcile.Result{}, fmt.Errorf("Uninstall: Waiting for Ceph RGW Route %v to be deleted", route.Name)

	}
	return reconcile.Result{}, nil
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
				r.Log.Info("Ceph RGW Route restore failed.", "CephRGWRoute", klog.KRef(route.Namespace, route.Name))
				return err
			}

			r.Log.Info("Restoring original Ceph RGW Route.", "CephRGWRoute", klog.KRef(route.Namespace, route.Name))
			existing.ObjectMeta.OwnerReferences = route.ObjectMeta.OwnerReferences
			route.ObjectMeta = existing.ObjectMeta
			err = r.Client.Update(context.TODO(), route)
			if err != nil {
				r.Log.Error(err, "Failed to update Ceph RGW Route Object.", "CephRGWRoute", klog.KRef(route.Namespace, route.Name))
				return err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating Ceph RGW Route.", "CephRGWRoute", klog.KRef(route.Namespace, route.Name))
			err = r.Client.Create(context.TODO(), route)
			if err != nil {
				r.Log.Error(err, "Failed to create Ceph RGW Route.", "CephRGWRoute", klog.KRef(route.Namespace, route.Name))
				return err
			}
		}
	}
	return nil
}

// newCephRGWRoutes returns the RGW route instances that should be created
// on first run.
func (r *StorageClusterReconciler) newCephRGWRoutes(initData *ocsv1.StorageCluster) ([]*routev1.Route, error) {
	// Use the same name as for the Ceph Object Store, two routes are exposed one with secure port, other with insecure port
	var insecureRoute = routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenerateNameForCephObjectStore(initData),
			Namespace: initData.Namespace,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: generateNameForCephObjectStoreService(initData),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "http",
				},
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationEdge,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyAllow,
			},
		},
	}

	var secureRoute = routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      util.GenerateNameForCephObjectStore(initData) + "-secure",
			Namespace: initData.Namespace,
		},
		Spec: routev1.RouteSpec{
			To: routev1.RouteTargetReference{
				Kind: "Service",
				Name: generateNameForCephObjectStoreService(initData),
			},
			Port: &routev1.RoutePort{
				TargetPort: intstr.IntOrString{
					Type:   intstr.String,
					StrVal: "https",
				},
			},
			TLS: &routev1.TLSConfig{
				Termination:                   routev1.TLSTerminationReencrypt,
				InsecureEdgeTerminationPolicy: routev1.InsecureEdgeTerminationPolicyRedirect,
			},
		},
	}

	ret := []*routev1.Route{}
	ret = append(ret, &secureRoute)
	// add http route only if DisableHttp is set to false
	if !initData.Spec.ManagedResources.CephObjectStores.DisableHttp {
		ret = append(ret, &insecureRoute)
	} else {
		r.Log.Info("skip creating insecure rgw http route", "CephRGWRoute", klog.KRef(insecureRoute.Namespace, insecureRoute.Name))
	}

	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Failed to set ControllerReference for Ceph RGW Route", "CephRGWRoute", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}
	return ret, nil
}

// deleteHttpRoute deletes the HTTP RGW route
func (r *StorageClusterReconciler) deleteHttpRoute(sc *ocsv1.StorageCluster) error {
	routeName := util.GenerateNameForCephObjectStore(sc)
	route := &routev1.Route{
		ObjectMeta: metav1.ObjectMeta{
			Name:      routeName,
			Namespace: sc.Namespace,
		},
	}

	r.Log.Info("Deleting http Ceph RGW Route.", "CephRGWRoute", klog.KRef(sc.Namespace, routeName))
	err := r.Client.Delete(context.TODO(), route)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Http Ceph RGW Route not found, nothing to delete.", "CephRGWRoute", klog.KRef(sc.Namespace, routeName))
			return nil
		}
		r.Log.Error(err, "Failed to delete http Ceph RGW Route.", "CephRGWRoute", klog.KRef(sc.Namespace, routeName))
		return fmt.Errorf("failed to delete http Ceph RGW Route %v: %v", routeName, err)
	}

	r.Log.Info("Successfully deleted http Ceph RGW Route.", "CephRGWRoute", klog.KRef(sc.Namespace, routeName))
	return nil
}

// generateNameForCephObjectStoreService is temporary - we should ideally get this name from rook
func generateNameForCephObjectStoreService(initData *ocsv1.StorageCluster) string {
	return fmt.Sprintf("%s-%s", "rook-ceph-rgw", util.GenerateNameForCephObjectStore(initData))
}
