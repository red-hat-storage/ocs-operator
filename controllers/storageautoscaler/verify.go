package storageautoscaler

import (
	"context"
	"errors"
	"fmt"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// verifyScaling verifies the scaling of the storage autoscaler
// Check the type of scaling happening and verify if the scaling is correct
// Query prometheus for the metrics and verify the scaling
// Check the expected size and count of the osd
// Change the status of the storage autoscaler to "completed" if the scaling is correct
// if the scaling is not successful, requeueAfter 1 minute
// Change the status of the storage autoscaler to "failed" if the scaling does not happen in timeSeconds
func (r *StorageAutoscalerReconciler) verifyScaling(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler) (reconcile.Result, error) {
	if timeoutHasElapsed(storageAutoScaler.Spec.TimeoutSeconds, storageAutoScaler.Status.LastExpansion.StartTime) {
		originalStorageAutoScaler := storageAutoScaler.DeepCopy()
		storageAutoScaler.Status.Phase = "Failed"
		storageAutoScaler.Status.Error = &ocsv1.TimestampedError{
			Message:   "scaling verification timed out for storageAutoScaler" + storageAutoScaler.Name + "device class " + storageAutoScaler.Spec.DeviceClass,
			Timestamp: metav1.Now(),
		}
		err := r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
		if err != nil {
			return reconcile.Result{}, err
		}
		err = fmt.Errorf("scaling verification timed out, for device class %s", storageAutoScaler.Spec.DeviceClass)
		r.Log.Error(err, "scaling verification timed out")
	}

	startOsdSize := storageAutoScaler.Status.LastExpansion.StartOsdSize
	expectedOsdSize := storageAutoScaler.Status.LastExpansion.ExpectedOsdSize
	expectedOsdCount := storageAutoScaler.Status.LastExpansion.ExpectedOsdCount
	expectedStorageCapacity := storageAutoScaler.Status.LastExpansion.ExpectedStorageCapacity

	if startOsdSize.Cmp(expectedOsdSize) != 0 {
		// vertical scaling
		err := r.verifyVerticalScaling(ctx, storageAutoScaler, expectedOsdSize)
		if err != nil {
			r.Log.Error(err, "failed to verify vertical scaling")
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
		}
	} else {
		// horizontal scaling
		err := r.verifyHorizontalScaling(ctx, storageAutoScaler, expectedOsdCount)
		if err != nil {
			r.Log.Error(err, "failed to verify horizontal scaling")
			return reconcile.Result{RequeueAfter: 1 * time.Minute}, nil
		}
	}

	// update the status
	originalStorageAutoScaler := storageAutoScaler.DeepCopy()
	storageAutoScaler.Status.Phase = "Succeeded"
	storageAutoScaler.Status.Error = nil
	storageAutoScaler.Status.LastExpansion.CompletionTime = metav1.Now()
	storageAutoScaler.Status.LastExpansion.StartOsdSize = expectedOsdSize
	storageAutoScaler.Status.LastExpansion.StartOsdCount = expectedOsdCount
	storageAutoScaler.Status.LastExpansion.StartStorageCapacity = expectedStorageCapacity

	err := r.updateStatus(ctx, storageAutoScaler, originalStorageAutoScaler)
	if err != nil {
		return reconcile.Result{}, err
	}
	r.Log.Info("scaling verified")

	return reconcile.Result{}, nil
}

func (r *StorageAutoscalerReconciler) verifyVerticalScaling(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, expectedOsdSize resource.Quantity) error {
	osdCountQuery := `(ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes))`
	metrics, err := scrapeMetrics(ctx, r.OperatorNamespace, osdCountQuery, r.Log)
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return err
	}

	verified := false
	for _, osd := range metrics {
		deviceClass := osd.Metric["device_class"]
		if string(deviceClass) == storageAutoScaler.Spec.DeviceClass {
			verified = true
			if expectedOsdSize.Cmp(resource.MustParse(osd.Value.String())) > 0 {
				r.Log.Info("osd size not as expected", "expected", expectedOsdSize, "actual", osd.Value)
				return errors.New("osd size not as expected, expected: " + expectedOsdSize.String() + " actual: " + osd.Value.String())
			}
			break
		}
	}

	if !verified {
		r.Log.Info("device class not found in metrics", "device class", storageAutoScaler.Spec.DeviceClass)
		return errors.New("device class not found in metrics, device class: " + storageAutoScaler.Spec.DeviceClass)
	}

	return nil
}

func (r *StorageAutoscalerReconciler) verifyHorizontalScaling(ctx context.Context, storageAutoScaler *ocsv1.StorageAutoScaler, expectedOsdCount uint16) error {
	osdCountQuery := `count by (device_class, namespace, managedBy) (ceph_osd_metadata)`
	metrics, err := scrapeMetrics(ctx, r.OperatorNamespace, osdCountQuery, r.Log)
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return err
	}

	verified := false
	for _, osd := range metrics {
		deviceClass := osd.Metric["device_class"]
		verified = true
		if string(deviceClass) == storageAutoScaler.Spec.DeviceClass {
			if float64(osd.Value) != float64(expectedOsdCount) {
				r.Log.Info("osd count not as expected", "expected", expectedOsdCount, "actual", osd.Value)
				return errors.New("osd count not as expected")
			}
			break
		}
	}

	if !verified {
		r.Log.Info("device class not found in metrics", "device class", storageAutoScaler.Spec.DeviceClass)
		return errors.New("device class not found in metrics, device class: " + storageAutoScaler.Spec.DeviceClass)
	}

	return nil
}

func timeoutHasElapsed(timeoutSeconds int, reconcileStartTime metav1.Time) bool {
	return time.Since(reconcileStartTime.Time).Seconds() >= float64(timeoutSeconds)
}
