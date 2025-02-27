package storagecluster

import (
	"context"
	"fmt"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	v1 "k8s.io/api/core/v1"
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
				Name:      util.GenerateNameForCephNFS(initData),
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
					LogLevel:          initData.Spec.NFS.LogLevel,
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
	reconcileStrategy := ReconcileStrategy(instance.Spec.NFS.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
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
			if instance.Spec.NFS.LogLevel != "" {
				existingCephNFS.Spec.Server.LogLevel = instance.Spec.NFS.LogLevel
			}
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
	if sc.Spec.NFS == nil || !sc.Spec.NFS.Enable {
		return reconcile.Result{}, nil
	}
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

type ocsCephNFSService struct{}

// newNFSServices returns the Service instances that should be created on first run.
func (r *StorageClusterReconciler) newNFSServices(initData *ocsv1.StorageCluster) ([]*v1.Service, error) {
	ret := []*v1.Service{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.GenerateNameForNFSService(initData),
				Namespace: initData.Namespace,
			},
			Spec: v1.ServiceSpec{
				Ports: []v1.ServicePort{
					{
						Name: "nfs",
						Port: 2049,
					},
					{
						Name: "nfs-metrics",
						Port: 9587,
					},
				},
				Selector: map[string]string{
					"app":      "rook-ceph-nfs",
					"ceph_nfs": util.GenerateNameForCephNFS(initData),
				},
				SessionAffinity: "ClientIP",
			},
		},
	}

	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set Controller Reference for NFS service.", " NFSService ", klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}

	return ret, nil
}

// newNFSMetricsServiceMonitors returns the ServiceMonitor instances that should be created on first run.
func (r *StorageClusterReconciler) newNFSMetricsServiceMonitors(initData *ocsv1.StorageCluster) (
	[]*monitoringv1.ServiceMonitor, error) {
	ret := []*monitoringv1.ServiceMonitor{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name:      util.GenerateNameForNFSServiceMonitor(initData),
				Namespace: initData.Namespace,
			},
			Spec: monitoringv1.ServiceMonitorSpec{
				NamespaceSelector: monitoringv1.NamespaceSelector{
					MatchNames: []string{initData.Namespace},
				},
				Endpoints: []monitoringv1.Endpoint{
					{
						Port:     "nfs-metrics",
						Path:     "/metrics",
						Interval: "20s",
					},
				},
				Selector: metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "rook-ceph-nfs",
					},
				},
			},
		},
	}

	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.Scheme)
		if err != nil {
			r.Log.Error(err, "Unable to set Controller Reference for NFS servicemonitor.", " NFSServiceMonitor ",
				klog.KRef(obj.Namespace, obj.Name))
			return nil, err
		}
	}

	return ret, nil
}

// ensureCreated ensures that cephNFS related services exist in the desired state.
func (obj *ocsCephNFSService) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	if instance.Spec.NFS == nil || !instance.Spec.NFS.Enable {
		return reconcile.Result{}, nil
	}

	nfsServices, err := r.newNFSServices(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	nfsMetricsServiceMonitors, err := r.newNFSMetricsServiceMonitors(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	ctxTODO := context.TODO()
	for _, nfsService := range nfsServices {
		existingNFSService := v1.Service{}
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsService.Name, Namespace: nfsService.Namespace}, &existingNFSService)
		switch {
		case err == nil:
			if existingNFSService.DeletionTimestamp != nil {
				r.Log.Info("Unable to restore NFS Service because it is marked for deletion.", "NFSService", klog.KRef(existingNFSService.Namespace, existingNFSService.Name))
				return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %q because it is marked for deletion", existingNFSService.Name)
			}

			r.Log.Info("Restoring original NFS service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
			existingNFSService.ObjectMeta.OwnerReferences = nfsService.ObjectMeta.OwnerReferences
			existingNFSService.Spec = nfsService.Spec
			err = r.Client.Update(ctxTODO, &existingNFSService)
			if err != nil {
				r.Log.Error(err, "Unable to update NFS service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
				return reconcile.Result{}, err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating NFS service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
			err = r.Client.Create(ctxTODO, nfsService)
			if err != nil {
				r.Log.Error(err, "Unable to create NFS service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Namespace))
				return reconcile.Result{}, err
			}
		default:
			r.Log.Error(err, fmt.Sprintf("Unable to retrieve NFS service %q.", nfsService.Name), "NFSService",
				klog.KRef(nfsService.Namespace, nfsService.Name))
			return reconcile.Result{}, fmt.Errorf("Unable to retrieve NFS service %q: %w", nfsService.Name, err)
		}
	}

	for _, nfsMetricsServiceMonitor := range nfsMetricsServiceMonitors {
		existingNFSMetricsServiceMonitor := monitoringv1.ServiceMonitor{}
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsMetricsServiceMonitor.Name,
			Namespace: nfsMetricsServiceMonitor.Namespace}, &existingNFSMetricsServiceMonitor)
		switch {
		case err == nil:
			if existingNFSMetricsServiceMonitor.DeletionTimestamp != nil {
				r.Log.Info("Unable to restore NFS-metrics ServiceMonitor because it is marked for deletion.", "NFSMetricsServiceMonitor",
					klog.KRef(existingNFSMetricsServiceMonitor.Namespace, existingNFSMetricsServiceMonitor.Name))
				return reconcile.Result{}, fmt.Errorf("failed to restore initialization object %q because it is marked for deletion",
					existingNFSMetricsServiceMonitor.Name)
			}

			r.Log.Info("Restoring original NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
				klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
			existingNFSMetricsServiceMonitor.ObjectMeta.OwnerReferences = nfsMetricsServiceMonitor.ObjectMeta.OwnerReferences
			existingNFSMetricsServiceMonitor.Spec = nfsMetricsServiceMonitor.Spec
			err = r.Client.Update(ctxTODO, &existingNFSMetricsServiceMonitor)
			if err != nil {
				r.Log.Error(err, "Unable to update NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
					klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
				return reconcile.Result{}, err
			}
		case errors.IsNotFound(err):
			r.Log.Info("Creating NFS servicemonitor.", "NFSMetricsServiceMonitor",
				klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
			err = r.Client.Create(ctxTODO, nfsMetricsServiceMonitor)
			if err != nil {
				r.Log.Error(err, "Unable to create NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
					klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Namespace))
				return reconcile.Result{}, err
			}
		default:
			r.Log.Error(err, fmt.Sprintf("Unable to retrieve NFS-metrics servicemonitor %q.", nfsMetricsServiceMonitor.Name),
				"NFSServiceMonitor", klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
			return reconcile.Result{}, fmt.Errorf("Unable to retrieve NFS-metrics servicemonitor %q: %w", nfsMetricsServiceMonitor.Name, err)
		}
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the cephNFS related services owned by the StorageCluster
func (obj *ocsCephNFSService) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	ctxTODO := context.TODO()
	foundNFSService := &v1.Service{}
	nfsServices, err := r.newNFSServices(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	nfsMetricsServiceMonitors, err := r.newNFSMetricsServiceMonitors(sc)
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, nfsService := range nfsServices {
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsService.Name, Namespace: sc.Namespace}, foundNFSService)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: NFS Service not found.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve NFS Service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve NFS Service %q: %v", nfsService.Name, err)
		}

		if nfsService.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting NFS Service.", "NFSService", klog.KRef(foundNFSService.Namespace, foundNFSService.Name))
			err = r.Client.Delete(ctxTODO, foundNFSService)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete NFS Service.", "NFSService", klog.KRef(foundNFSService.Namespace, foundNFSService.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete NFS Service %q: %v", foundNFSService.Name, err)
			}
		}

		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsService.Name, Namespace: sc.Namespace}, foundNFSService)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: NFS Service is deleted.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve NFS Service.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve NFS Service %q: %v", nfsService.Name, err)
		}

		err = fmt.Errorf("NFS Service %q still exists", nfsService.Name)
		r.Log.Error(err, "Uninstall: Waiting for NFS Service to be deleted.", "NFSService", klog.KRef(nfsService.Namespace, nfsService.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for NFS Service %q to be deleted", nfsService.Name)
	}

	for _, nfsMetricsServiceMonitor := range nfsMetricsServiceMonitors {
		foundNFSMetricsServiceMonitor := &monitoringv1.ServiceMonitor{}
		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsMetricsServiceMonitor.Name, Namespace: sc.Namespace},
			foundNFSMetricsServiceMonitor)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: NFS-metrics servicemonitor not found.", "NFSMetricsServiceMonitor",
					klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
				klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve NFS-metrics servicemonitor %q: %v",
				nfsMetricsServiceMonitor.Name, err)
		}

		if nfsMetricsServiceMonitor.GetDeletionTimestamp().IsZero() {
			r.Log.Info("Uninstall: Deleting NFS Service.", "NFSMetricsServiceMonitor",
				klog.KRef(foundNFSMetricsServiceMonitor.Namespace, foundNFSMetricsServiceMonitor.Name))
			err = r.Client.Delete(ctxTODO, foundNFSMetricsServiceMonitor)
			if err != nil {
				r.Log.Error(err, "Uninstall: Failed to delete NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
					klog.KRef(foundNFSMetricsServiceMonitor.Namespace, foundNFSMetricsServiceMonitor.Name))
				return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete NFS-metrics servicemonitor %q: %v",
					foundNFSMetricsServiceMonitor.Name, err)
			}
		}

		err = r.Client.Get(ctxTODO, types.NamespacedName{Name: nfsMetricsServiceMonitor.Name, Namespace: sc.Namespace}, foundNFSMetricsServiceMonitor)
		if err != nil {
			if errors.IsNotFound(err) {
				r.Log.Info("Uninstall: NFS-metrics servicemonitor is deleted.", "NFSMetricsServiceMonitor",
					klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
				continue
			}
			r.Log.Error(err, "Uninstall: Unable to retrieve NFS-metrics servicemonitor.", "NFSMetricsServiceMonitor",
				klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
			return reconcile.Result{}, fmt.Errorf("uninstall: Unable to retrieve NFS-metrics ServiceMonitor %q: %v",
				nfsMetricsServiceMonitor.Name, err)
		}

		err = fmt.Errorf("NFS-metrics ServiceMonitor %q still exists", nfsMetricsServiceMonitor.Name)
		r.Log.Error(err, "Uninstall: Waiting for NFS-metrics servicemonitor to be deleted.", "NFSMetricsServiceMonitor",
			klog.KRef(nfsMetricsServiceMonitor.Namespace, nfsMetricsServiceMonitor.Name))
		return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for NFS-metrics servicemonitor %q to be deleted",
			nfsMetricsServiceMonitor.Name)
	}

	return reconcile.Result{}, nil
}
