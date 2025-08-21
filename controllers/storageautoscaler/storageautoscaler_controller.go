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
	// get the eventCh from the storage autoscaler scraper
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageAutoScaler{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		// watch for generic events to trigger the reconcile
		WatchesRawSource(source.Channel(r.EventCh,
			&handler.EnqueueRequestForObject{},
		)).Complete(r)
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

	// if previously it was in invalid state, update the status to not started
	if storageAutoScaler.Status.Phase == ocsv1.StorageAutoScalerPhaseInvalid {
		storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseNotStarted
		storageAutoScaler.Status.Error = nil
		storageAutoScaler.Status.LastExpansion = nil
		err := r.updateStatus(ctx, storageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// if no expansion is in-progress update the phase to "not-started"
	if storageAutoScaler.Status.Phase == "" {
		storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseNotStarted
		err := r.updateStatus(ctx, storageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	// get the current osd size and count from prometheus
	currentOsdSize, currentOsdCount, err := r.getCurrentOsdSizeAndCount(ctx, storageAutoScaler)
	if err != nil {
		r.Log.Error(err, "failed to get current osd size and count")
		return reconcile.Result{}, err
	}

	// get the desired osd size and count from storagecluster
	desiredOsdSize, desiredOsdCount := getDesiredOsdSizeAndCount(storageCluster, storageAutoScaler)

	// expansion is InProgress
	if currentOsdSize.Cmp(desiredOsdSize) != 0 || currentOsdCount != desiredOsdCount {
		return r.reverifyScaling(ctx, storageAutoScaler, desiredOsdSize, desiredOsdCount)
	}

	// if the expansion is Succeeded
	if storageAutoScaler.Status.Phase == ocsv1.StorageAutoScalerPhaseFailed || storageAutoScaler.Status.Phase == ocsv1.StorageAutoScalerPhaseInProgress {
		return r.updatePhaseToSucceeded(ctx, storageAutoScaler)
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

	if !isScalingRequired(r.SyncMap, storageAutoScaler.Spec.StorageScalingThresholdPercent, r.Log, storageAutoScaler.Spec.DeviceClass) {
		r.Log.Info("scaling is not required", "device class", storageAutoScaler.Spec.DeviceClass)
		return reconcile.Result{}, nil
	}
	// scaling is required
	r.Log.Info("scaling is required", "device class", storageAutoScaler.Spec.DeviceClass)

	// calculate the expectedStorageCapacity
	startOsdSize, expectedOsdSize, startOsdCount, expectedOsdCount, startStorageCapacity, expectedStorageCapacity := calculateExpectedOsdSizeAndCount(storageCluster, storageAutoScaler)

	if expectedStorageCapacity.Cmp(storageAutoScaler.Spec.StorageCapacityLimit) > 0 {
		r.Log.Info("storage capacity limit reached")
		// update the status
		bool := true
		storageAutoScaler.Status.StorageCapacityLimitReached = &bool
		storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
			Message:   fmt.Sprintf("storage capacity limit reached for storageAutoScaler %q device class %q least expected storage capacity is %q", storageAutoScaler.Name, storageAutoScaler.Spec.DeviceClass, expectedStorageCapacity.String()),
			Timestamp: metav1.Now(),
		}
		err := r.updateStatus(ctx, storageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, nil
	}

	// update the status
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
	err = r.updateStatus(ctx, storageAutoScaler)
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

	return reconcile.Result{Requeue: true}, nil
}

func (r *StorageAutoscalerReconciler) updateStatus(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler) error {
	if err := r.Status().Update(ctx, storageAutoScaler); err != nil {
		r.Log.Error(err, "failed to update storage autoscaler status")
		return err
	}
	r.Log.Info("storage autoscaler status updated")
	return nil
}

func (r *StorageAutoscalerReconciler) detectInvalidState(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, storageCluster *ocsv1.StorageCluster, namespace string) (bool, error) {
	// check if it's a external mode storage cluster
	externalMode, err := r.detectExternalModeStorageCluster(ctx, storageCluster, storageAutoScaler)
	if err != nil {
		return false, err
	}
	if externalMode {
		return true, nil
	}

	// if more than one storage autoscaler is present in the cluster with same device class and same storage cluster
	// return error
	duplicateCr, err := r.detectDuplicateStorageAutoScaler(ctx, storageAutoScaler, namespace)
	if err != nil {
		return false, err
	}
	if duplicateCr {
		return true, nil
	}

	// list the storagecluster deviceset storageclass
	duplicateClass, err := r.detectLsoStorageclass(ctx, storageCluster, storageAutoScaler)
	if err != nil {
		return false, err
	}
	if duplicateClass {
		return true, nil
	}

	// check if the storage profile is lean
	leanProfile, err := r.detectLeanResourceProfile(ctx, storageCluster, storageAutoScaler)
	if err != nil {
		return false, err
	}
	if leanProfile {
		return true, nil
	}

	r.Log.Info("storage autoscaler is in invalid state", "namespace", storageAutoScaler.Namespace, "name", storageAutoScaler.Name)
	return false, nil
}

func (r *StorageAutoscalerReconciler) detectLsoStorageclass(ctx context.Context, storageCluster *ocsv1.StorageCluster, storageAutoScaler *ocsv1.StorageAutoScaler) (bool, error) {
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
			err = fmt.Errorf("storage class %q has no provisioner", storageClass.Name)
			r.Log.Error(err, "storage class has provisioner as no-provisioner, which is an lso storageclass, autoscaler does not support lso storageclass, delete the autoStorageScaler cr as scaling is not supported")

			// update the status
			storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInvalid
			storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
				Message:   fmt.Sprintf("storage class %q has provisioner as no-provisioner, which is an lso storageclass, autoscaler does not support lso storageclass, delete the autoStorageScaler cr as scaling is not supported", storageClass.Name),
				Timestamp: metav1.Now(),
			}
			err := r.updateStatus(ctx, storageAutoScaler)
			if err != nil {
				return false, err
			}
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
			err = fmt.Errorf("more than one storage autoscaler present with same device class and same storage cluster name, names are %q and %q", storageAutoScaler.Name, storageAutoScalerItem.Name)
			r.Log.Error(err, "duplicate cr detected", "device class", storageAutoScaler.Spec.DeviceClass, "storage cluster", storageAutoScaler.Spec.StorageCluster.Name)

			storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInvalid
			storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
				Message:   fmt.Sprintf("duplicate cr detected, more than one storage autoscaler present with same device class and same storage cluster name, names are %q and %q delete any one of the autoStorageScaler cr", storageAutoScaler.Name, storageAutoScalerItem.Name),
				Timestamp: metav1.Now(),
			}
			err := r.updateStatus(ctx, storageAutoScaler)
			if err != nil {
				return false, err
			}
			// not sending an error as user can see in the cr status
			return true, nil
		}
	}

	return false, nil
}

func (r *StorageAutoscalerReconciler) detectLeanResourceProfile(ctx context.Context, storageCluster *ocsv1.StorageCluster, storageAutoScaler *ocsv1.StorageAutoScaler) (bool, error) {
	if strings.ToLower(storageCluster.Spec.ResourceProfile) != "lean" {
		return false, nil
	}

	err := fmt.Errorf("storage profile is lean, autoscaler does not support lean storage profile, delete the autoStorageScaler cr as scaling is not supported")
	r.Log.Error(err, "storage profile is lean")

	storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInvalid
	storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
		Message:   "storage profile is lean, autoscaler does not support lean storage profile, delete the autoStorageScaler cr as scaling is not supported",
		Timestamp: metav1.Now(),
	}
	err = r.updateStatus(ctx, storageAutoScaler)
	if err != nil {
		return false, err
	}
	// not sending an error as user can see in the cr status
	return true, nil
}

func (r *StorageAutoscalerReconciler) detectExternalModeStorageCluster(ctx context.Context, storageCluster *ocsv1.StorageCluster, storageAutoScaler *ocsv1.StorageAutoScaler) (bool, error) {
	if !storageCluster.Spec.ExternalStorage.Enable {
		return false, nil
	}

	err := fmt.Errorf("storage cluster is in external mode, autoscaler does not support external mode, delete the autoStorageScaler cr as scaling is not supported")
	r.Log.Error(err, "storage cluster is external mode")

	storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseInvalid
	storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
		Message:   "storage cluster is in external mode, autoscaler does not support external mode, delete the autoStorageScaler cr as scaling is not supported",
		Timestamp: metav1.Now(),
	}
	err = r.updateStatus(ctx, storageAutoScaler)
	if err != nil {
		return false, err
	}
	// not sending an error as user can see in the cr status
	return true, nil
}

func (r *StorageAutoscalerReconciler) getCurrentOsdSizeAndCount(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler) (resource.Quantity, int, error) {
	currentOsdCount := 0
	currentOsdSize := resource.Quantity{}
	osdCountQuery := `count by (device_class, namespace, managedBy) (ceph_osd_metadata)`
	metrics, err := scrapeMetrics(ctx, r.OperatorNamespace, osdCountQuery, r.Log)
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return currentOsdSize, currentOsdCount, err
	}

	for _, osd := range metrics {
		deviceClass := osd.Metric["device_class"]
		verified := true
		if string(deviceClass) == storageAutoScaler.Spec.DeviceClass {
			currentOsdCount = int(osd.Value)
			break
		}

		if !verified {
			r.Log.Info("device class not found in metrics", "device class", storageAutoScaler.Spec.DeviceClass)
			return currentOsdSize, currentOsdCount, fmt.Errorf("device class not found in metrics, device class: %q", storageAutoScaler.Spec.DeviceClass)
		}
	}

	osdSizeQuery := `(ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes))`
	metrics, err = scrapeMetrics(ctx, r.OperatorNamespace, osdSizeQuery, r.Log)
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return currentOsdSize, currentOsdCount, err
	}

	for _, osd := range metrics {
		deviceClass := osd.Metric["device_class"]
		verified := true
		if string(deviceClass) == storageAutoScaler.Spec.DeviceClass {
			currentOsdSize = resource.MustParse(osd.Value.String())
			break
		}

		if !verified {
			r.Log.Info("device class not found in metrics", "device class", storageAutoScaler.Spec.DeviceClass)
			return currentOsdSize, currentOsdCount, fmt.Errorf("device class not found in metrics, device class: %q", storageAutoScaler.Spec.DeviceClass)
		}
	}

	return currentOsdSize, currentOsdCount, nil
}

func getDesiredOsdSizeAndCount(storageCluster *ocsv1.StorageCluster, storageAutoScaler *ocsv1.StorageAutoScaler) (resource.Quantity, int) {
	var desiredOsdSize resource.Quantity
	var desiredOsdCount, deviceSetCount, replicaCount int

	// assuming heterogeneous distribution of osd across device sets
	// so osd size and count is same for all device sets
	for _, deviceSet := range storageCluster.Spec.StorageDeviceSets {
		deviceClass := storageAutoScaler.Spec.DeviceClass
		if deviceClassMatchesDeviceSet(deviceClass, deviceSet) {
			desiredOsdSize = deviceSet.DataPVCTemplate.Spec.Resources.Requests["storage"]
			replicaCount = deviceSet.Replica
			desiredOsdCount = deviceSet.Count * replicaCount
			deviceSetCount++
		}
	}

	return desiredOsdSize, desiredOsdCount * deviceSetCount
}

func (r *StorageAutoscalerReconciler) reverifyScaling(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, desiredOsdSize resource.Quantity, desiredOsdCount int) (reconcile.Result, error) {
	if timeoutHasElapsed(storageAutoScaler.Spec.TimeoutSeconds, storageAutoScaler.Status.LastExpansion) {
		storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseFailed
		storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
			Message:   fmt.Sprintf("auto scaling is taking longer than expected and may not succeed for storageAutoScaler %q device class %q", storageAutoScaler.Name, storageAutoScaler.Spec.DeviceClass),
			Timestamp: metav1.Now(),
		}
		err := r.updateStatus(ctx, storageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
		err = fmt.Errorf("auto scaling is taking longer than expected and may not succeed, for device class %q", storageAutoScaler.Spec.DeviceClass)
		r.Log.Error(err, "auto scaling verification failed, will retry")
	}

	// if storagecluster is manually edited, change the desired osd size and count in the status
	if storageAutoScaler.Status.LastExpansion != nil {
		if storageAutoScaler.Status.LastExpansion.ExpectedOsdSize.Cmp(desiredOsdSize) != 0 || storageAutoScaler.Status.LastExpansion.ExpectedOsdCount != uint16(desiredOsdCount) {
			storageAutoScaler.Status.LastExpansion.ExpectedOsdSize = desiredOsdSize
			storageAutoScaler.Status.LastExpansion.ExpectedOsdCount = uint16(desiredOsdCount)
			storageAutoScaler.Status.LastExpansion.ExpectedStorageCapacity = *resource.NewQuantity(int64(desiredOsdCount)*desiredOsdSize.Value(), desiredOsdSize.Format)
			err := r.updateStatus(ctx, storageAutoScaler)
			if err != nil {
				return reconcile.Result{}, err
			}
			r.Log.Info("storage cluster is manually edited, updating the desired osd size and count in the status")
		}
	}

	return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil

}

func (r *StorageAutoscalerReconciler) updatePhaseToSucceeded(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler) (reconcile.Result, error) {
	storageAutoScaler.Status.Phase = ocsv1.StorageAutoScalerPhaseSucceeded
	storageAutoScaler.Status.Error = nil
	storageAutoScaler.Status.LastExpansion.CompletionTime = metav1.Now()
	storageAutoScaler.Status.LastExpansion.StartOsdSize = storageAutoScaler.Status.LastExpansion.ExpectedOsdSize
	storageAutoScaler.Status.LastExpansion.StartOsdCount = storageAutoScaler.Status.LastExpansion.ExpectedOsdCount
	storageAutoScaler.Status.LastExpansion.StartStorageCapacity = storageAutoScaler.Status.LastExpansion.ExpectedStorageCapacity

	err := r.updateStatus(ctx, storageAutoScaler)
	if err != nil {
		return reconcile.Result{}, err
	}

	r.Log.Info("scaling verified")
	return reconcile.Result{}, nil
}

func timeoutHasElapsed(timeoutSeconds int, lastExpansion *ocsv1.LastExpansionStatus) bool {
	if lastExpansion == nil {
		return false
	}

	return time.Since(lastExpansion.StartTime.Time).Seconds() >= float64(timeoutSeconds)
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

func deviceClassMatchesDeviceSet(deviceClass string, deviceSet ocsv1.StorageDeviceSet) bool {
	return (deviceSet.DeviceClass == "" && deviceClass == "ssd") || deviceSet.DeviceClass == deviceClass
}
