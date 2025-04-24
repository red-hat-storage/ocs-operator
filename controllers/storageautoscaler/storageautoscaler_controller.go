package storageautoscaler

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	storagev1 "k8s.io/api/storage/v1"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

// StorageAutoscalerReconciler is the reconciler for StorageAutoscaler objects
type StorageAutoscalerReconciler struct {
	client.Client
	Log               logr.Logger
	OperatorNamespace string
	SyncMap           *sync.Map
	EventCh           chan event.GenericEvent
	ScrapeInterval    time.Duration
}

// SetupWithManager sets up the reconciler with the manager
func (r *StorageAutoscalerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueAutoScaler := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			storageCluster, ok := obj.(*ocsv1.StorageCluster)
			if !ok {
				return nil
			}

			// list the storage autoscaler objects
			storageAutoScalerList := &ocsv1.StorageAutoScalerList{}
			err := r.List(context, storageAutoScalerList,
				client.InNamespace(storageCluster.Namespace),
			)
			if err != nil {
				r.Log.Error(err, "failed to list storage autoscaler")
				return nil
			}
			// create a reconcile request for each storage autoscaler object
			requests := make([]reconcile.Request, 0, len(storageAutoScalerList.Items))
			for _, storageAutoScaler := range storageAutoScalerList.Items {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: storageAutoScaler.Namespace,
						Name:      storageAutoScaler.Name,
					},
				})
			}
			return requests
		},
	)

	osdPredicate := predicate.Funcs{
		UpdateFunc: func(e event.UpdateEvent) bool {
			oldStorageCluster, ok := e.ObjectOld.(*ocsv1.StorageCluster)
			if !ok {
				return false
			}
			newStorageCluster, ok := e.ObjectNew.(*ocsv1.StorageCluster)
			if !ok {
				return false
			}

			// check if the osd size or count or replica has changed
			for i, deviceSet := range oldStorageCluster.Spec.StorageDeviceSets {
				if deviceSet.Count != newStorageCluster.Spec.StorageDeviceSets[i].Count {
					return true
				}
				oldOsdSize := deviceSet.DataPVCTemplate.Spec.Resources.Requests["storage"]
				newOsdSize := newStorageCluster.Spec.StorageDeviceSets[i].DataPVCTemplate.Spec.Resources.Requests["storage"]
				if oldOsdSize.Cmp(newOsdSize) != 0 {
					return true
				}
				if deviceSet.Replica != newStorageCluster.Spec.StorageDeviceSets[i].Replica {
					return true
				}
			}

			return false
		},
	}

	// get the eventCh from the storage autoscaler scraper
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageAutoScaler{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// watch for generic events to trigger the reconcile
		WatchesRawSource(source.Channel(r.EventCh,
			&handler.EnqueueRequestForObject{},
		)).
		// watch for storagecluster osd size and count changes
		Watches(&ocsv1.StorageCluster{}, enqueueAutoScaler, builder.WithPredicates(osdPredicate)).
		Complete(r)
}

// +kubebuilder:rbac:groups="monitoring.coreos.com",resources=prometheuses/api,resourceNames=k8s,verbs=get

// Reconcile reconciles the StorageAutoscaler object
func (r *StorageAutoscalerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	r.Log.Info("reconciling storage autoscaler", "namespace", request.Namespace, "name", request.Name)

	// list the storage autoscaler reconciler object
	storageAutoScaler := &ocsv1.StorageAutoScaler{}
	err := r.Get(ctx, types.NamespacedName{Namespace: request.Namespace, Name: request.Name}, storageAutoScaler)
	if err != nil {
		if kerrors.IsNotFound(err) {
			r.Log.Info("storage autoscaler not found for reconcile request", "namespace", request.Namespace, "name", request.Name)
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "failed to get storage autoscaler")
		return reconcile.Result{}, err
	}

	// list the storagecluster
	storageCluster := &ocsv1.StorageCluster{}
	storageClusterName := storageAutoScaler.Spec.StorageCluster.Name
	err = r.Get(ctx, types.NamespacedName{Name: storageClusterName, Namespace: request.Namespace}, storageCluster)
	if err != nil {
		r.Log.Error(err, "failed to get storage cluster")
		return reconcile.Result{}, err
	}

	// detect invalid state
	invalidState, err := r.detectInvalidState(ctx, storageAutoScaler, storageCluster, request.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}
	if invalidState {
		return reconcile.Result{}, nil
	}

	// if no expansion is in-progress update the phase to "not-started"
	if storageAutoScaler.Status.Phase == "" {
		originalStorageAutoScaler := storageAutoScaler.DeepCopy()
		storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseNotStarted
		err := r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// if the expansion is InProgress or Failed, verify the scaling
	if storageAutoScaler.Status.Phase == ocsv1.StorageAutoScalerPhaseInProgress || storageAutoScaler.Status.Phase == ocsv1.StorageAutoScalerPhaseFailed {
		return r.verifyScaling(ctx, storageAutoScaler)
	}

	// if the last expansion is succeeded ScrapeInterval ago, do not scale
	if storageAutoScaler.Status.LastExpansion != nil {
		timeSince := time.Since(storageAutoScaler.Status.LastExpansion.CompletionTime.Time)
		if timeSince.Seconds() < r.ScrapeInterval.Seconds() {
			r.Log.Info("last expansion was succeeded less than ScrapeInterval ago", "ScrapeInterval", r.ScrapeInterval, "time since last expansion", time.Since(storageAutoScaler.Status.LastExpansion.CompletionTime.Time))
			// return nil error as scraper will retry if needed
			return reconcile.Result{}, nil
		}
	}

	// check if the ceph cluster is healthy
	notHealthy, err := r.checkIfCephClusterIsNotHealthy(ctx, storageCluster)
	if err != nil {
		r.Log.Error(err, "failed to check if ceph cluster is healthy")
		return reconcile.Result{}, err
	}
	if notHealthy {
		err = fmt.Errorf("ceph cluster is not healthy")
		r.Log.Error(err, "scaling cannot be performed")
		// return nil error as scraper will retry if needed
		return reconcile.Result{}, nil
	}

	// get the osd usage from the sync map
	usage, ok := r.SyncMap.Load(storageAutoScaler.Spec.DeviceClass)
	if !ok {
		r.Log.Info("osd usage not found for device class in sync map", "device class", storageAutoScaler.Spec.DeviceClass)
		// return nil error as scraper will retry if needed
		return reconcile.Result{}, nil
	}

	if usage == nil {
		r.Log.Info("osd usage is nil for device class in sync map", "device class", storageAutoScaler.Spec.DeviceClass)
		// return nil error as scraper will retry if needed
		return reconcile.Result{}, nil
	}

	if !checkIfScalingRequired(usage, storageAutoScaler.Spec.StorageScalingThresholdPercent) {
		r.Log.Info("osd usage is less than the threshold", "device class", storageAutoScaler.Spec.DeviceClass, "usage", usage.(float64)*100)
		return reconcile.Result{}, nil
	}
	// scaling is required
	r.Log.Info("scaling is required", "device class", storageAutoScaler.Spec.DeviceClass)

	// calculate the expectedStorageCapacity
	startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity := calculateExpectedOsdSizeAndCount(storageCluster, storageAutoScaler)

	if expectedStorageCapacity.Cmp(storageAutoScaler.Spec.StorageCapacityLimit) > 0 {
		r.Log.Info("storage capacity limit reached")
		originalStorageAutoScaler := storageAutoScaler.DeepCopy()
		// update the status
		bool := true
		storageAutoScaler.Status.StorageCapacityLimitReached = &bool
		storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
			Message:   "storage capacity limit reached for storageAutoScaler" + storageAutoScaler.Name + "device class" + storageAutoScaler.Spec.DeviceClass + "least expected storage capacity is " + expectedStorageCapacity.String(),
			Timestamp: metav1.Now(),
		}
		err := r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// update the status
	originalStorageAutoScaler := storageAutoScaler.DeepCopy()
	storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInProgress
	storageAutoScaler.Status.Error = nil
	storageAutoScaler.Status.LastExpansion = &ocsv1.LastExpansionStatus{
		StartOsdCount:           uint16(startOsdCount),
		ExpectedOsdCount:        uint16(expectedOsdCount),
		StartOsdSize:            startOsdSize,
		ExpectedOsdSize:         expectedOsdSize,
		StartStorageCapacity:    startStorageCapacity,
		ExpectedStorageCapacity: expectedStorageCapacity,
		StartTime:               metav1.Now(),
		CompletionTime:          metav1.Time{},
	}
	err = r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
	if err != nil {
		return reconcile.Result{}, err
	}

	// patch the storagecluster with the expected osd size and count
	originalStorageCluster := storageCluster.DeepCopy()
	for i, deviceSet := range storageCluster.Spec.StorageDeviceSets {
		deviceClass := storageAutoScaler.Spec.DeviceClass
		if deviceClassMatchesDeviceSet(deviceClass, deviceSet) {
			storageCluster.Spec.StorageDeviceSets[i].DataPVCTemplate.Spec.Resources.Requests["storage"] = expectedOsdSize
			if startOsdCount != expectedOsdCount {
				storageCluster.Spec.StorageDeviceSets[i].Count = storageCluster.Spec.StorageDeviceSets[i].Count + 1
			}
			r.Log.Info("scaling storage cluster", "device set", storageCluster.Spec.StorageDeviceSets[i])
		}
	}

	err = r.Patch(ctx, storageCluster, client.MergeFrom(originalStorageCluster))
	if err != nil {
		r.Log.Error(err, "failed to update storage cluster")
		return reconcile.Result{}, err
	}

	return r.verifyScaling(ctx, storageAutoScaler)
}

func checkIfScalingRequired(usage interface{}, threshold int) bool {
	// check if the highest osd usage is greater than or equal to the threshold
	return ((usage.(float64) * 100) >= float64(threshold))
}

func calculateExpectedOsdSizeAndCount(storageCluster *ocsv1.StorageCluster, storageAutoScaler *ocsv1.StorageAutoScaler) (resource.Quantity, resource.Quantity, int, int, resource.Quantity, resource.Quantity) {
	var startOsdSize, expectedOsdSize resource.Quantity
	var startOsdCount, expectedOsdCount, deviceSetCount, replicaCount int

	// assuming heterogeneous distribution of osd across device sets
	// so osd size and count is same for all device sets
	for _, deviceSet := range storageCluster.Spec.StorageDeviceSets {
		deviceClass := storageAutoScaler.Spec.DeviceClass
		if deviceClassMatchesDeviceSet(deviceClass, deviceSet) {
			startOsdSize = deviceSet.DataPVCTemplate.Spec.Resources.Requests["storage"]
			replicaCount = deviceSet.Replica
			startOsdCount = deviceSet.Count * replicaCount
			deviceSetCount++
		}
	}

	expectedOsdSize = startOsdSize
	expectedOsdCount = startOsdCount

	startStorageCapacity := *resource.NewQuantity(int64(deviceSetCount*startOsdCount)*startOsdSize.Value(), startOsdSize.Format)

	if (resource.NewQuantity(startOsdSize.Value()*2, startOsdSize.Format)).Cmp(storageAutoScaler.Spec.MaxOsdSize) <= 0 {
		// vertical scaling
		// double the osd size
		expectedOsdSize = *resource.NewQuantity(startOsdSize.Value()*2, startOsdSize.Format)
	} else {
		// horizontal scaling
		expectedOsdCount = ((startOsdCount / replicaCount) + 1) * replicaCount
	}

	expectedStorageCapacity := *resource.NewQuantity(int64(deviceSetCount*expectedOsdCount)*expectedOsdSize.Value(), startOsdSize.Format)

	return startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity
}

func (r *StorageAutoscalerReconciler) checkIfCephClusterIsNotHealthy(ctx context.Context, storagecluster *ocsv1.StorageCluster) (bool, error) {
	cephHealthStatus := `ceph_health_status`
	metrics, err := scrapeMetrics(ctx, r.OperatorNamespace, cephHealthStatus, r.Log)
	if err != nil {
		r.Log.Info("failed to scrape metrics")
		return false, err
	}

	cephClusterPresent := false

	for _, metric := range metrics {
		if string(metric.Metric["namespace"]) == storagecluster.Namespace {
			cephClusterPresent = true
			if int(metric.Value) != 0 {
				r.Log.Info("ceph cluster is not healthy", "ceph_health_status", metric.Value)
				return true, nil
			}
		}
	}

	if !cephClusterPresent {
		r.Log.Info("ceph cluster not found in metrics")
		return false, errors.New("ceph cluster not found in metrics")
	}

	return false, nil
}

func deviceClassMatchesDeviceSet(deviceClass string, deviceSet ocsv1.StorageDeviceSet) bool {
	return (deviceSet.DeviceClass == "" && deviceClass == "ssd") || deviceSet.DeviceClass == deviceClass
}

func (r *StorageAutoscalerReconciler) updateStatus(ctx context.Context, storageAutoScaler, originalStorageAutoScaler *ocsv1.StorageAutoScaler) error {
	if err := r.Status().Patch(ctx, storageAutoScaler, client.MergeFrom(originalStorageAutoScaler)); err != nil {
		r.Log.Error(err, "failed to update storage autoscaler status")
		return err
	}
	r.Log.Info("storage autoscaler status updated")
	return nil
}

func (r *StorageAutoscalerReconciler) detectInvalidState(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, storageCluster *ocsv1.StorageCluster, namespace string) (bool, error) {
	// if more than one storage autoscaler is present in the cluster with same device class and same storage cluster
	// return error
	duplicateCr, err := r.detectDuplicateStorageAutoScaler(ctx, storageAutoScaler, namespace)
	if err != nil {
		return false, err
	}

	// list the storagecluster deviceset storageclass
	duplicateClass, err := r.detectLsoStorageclass(ctx, storageCluster)
	if err != nil {
		return false, err
	}

	// check if the storage profile is lean
	leanProfile, err := r.detectLeanResourceProfile(storageCluster)
	if err != nil {
		return false, err
	}

	if duplicateCr || duplicateClass || leanProfile {
		// update the status
		originalStorageAutoScaler := storageAutoScaler.DeepCopy()
		storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInvalid
		err := r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
		if err != nil {
			return false, err
		}
		r.Log.Info("storage autoscaler is in invalid state", "namespace", storageAutoScaler.Namespace, "name", storageAutoScaler.Name)
		return true, nil
	}

	return false, nil
}

func (r *StorageAutoscalerReconciler) detectLsoStorageclass(ctx context.Context, storageCluster *ocsv1.StorageCluster) (bool, error) {
	for _, deviceSet := range storageCluster.Spec.StorageDeviceSets {
		storageclassName := deviceSet.DataPVCTemplate.Spec.StorageClassName
		// list the storageclass
		storageClass := &storagev1.StorageClass{}
		err := r.Get(ctx, types.NamespacedName{Name: *storageclassName}, storageClass)
		if err != nil {
			r.Log.Error(err, "failed to get storage class")
			return false, err
		}
		if storageClass.Provisioner == "kubernetes.io/no-provisioner" {
			err = fmt.Errorf("storage class %s has no provisioner", storageClass.Name)
			r.Log.Error(err, "storage class has provisioner as no-provisioner, which is an lso storageclass, autoscaler does not support lso storageclass, delete the autoStorageScaler cr as scaling is not supported")
			// not sending an error as user can see in the cr status
			return true, nil
		}
	}

	return false, nil
}

func (r *StorageAutoscalerReconciler) detectDuplicateStorageAutoScaler(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, namespace string) (bool, error) {
	storageAutoScalerList := &ocsv1.StorageAutoScalerList{}
	err := r.List(ctx, storageAutoScalerList,
		client.InNamespace(namespace),
	)
	if err != nil {
		r.Log.Error(err, "failed to list storage autoscaler")
		return false, err
	}

	for _, storageAutoScalerItem := range storageAutoScalerList.Items {
		if storageAutoScalerItem.Name != storageAutoScaler.Name && storageAutoScalerItem.Spec.DeviceClass == storageAutoScaler.Spec.DeviceClass && storageAutoScalerItem.Spec.StorageCluster.Name == storageAutoScaler.Spec.StorageCluster.Name {
			err = fmt.Errorf("more than one storage autoscaler present with same device class and same storage cluster name, names are %s and %s", storageAutoScaler.Name, storageAutoScalerItem.Name)
			r.Log.Error(err, "duplicate cr detected", "device class", storageAutoScaler.Spec.DeviceClass, "storage cluster", storageAutoScaler.Spec.StorageCluster.Name)
			// not sending an error as user can see in the cr status
			return true, nil
		}
	}

	return false, nil
}

func (r *StorageAutoscalerReconciler) detectLeanResourceProfile(storageCluster *ocsv1.StorageCluster) (bool, error) {
	if strings.ToLower(storageCluster.Spec.ResourceProfile) != "lean" {
		return false, nil
	}

	err := fmt.Errorf("storage profile is lean, autoscaler does not support lean storage profile, delete the autoStorageScaler cr as scaling is not supported")
	r.Log.Error(err, "storage profile is lean")
	// not sending an error as user can see in the cr status
	return true, nil
}
