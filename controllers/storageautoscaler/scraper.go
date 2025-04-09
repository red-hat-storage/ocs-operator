package storageautoscaler

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/prometheus/common/model"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
)

// StorageAutoscalerScrpaer is the scraper for Prometheus metrics
type StorageAutoscalerScraper struct {
	client.Client
	Log               logr.Logger
	OperatorNamespace string
	ScrapeInterval    time.Duration
	SyncMap           *sync.Map
	EventCh           chan event.GenericEvent
}

func (r *StorageAutoscalerScraper) ScrapeMetricsPeriodically(ctx context.Context) {
	for {
		select {
		// context of the main reconcile loop is cancelled
		case <-ctx.Done():
			r.Log.Info("context cancelled, stopping scraping metrics")
			return

		case <-time.After(r.ScrapeInterval):
			r.enqueueGenericEventsFromMetrics(ctx)
		}
	}
}

func (r *StorageAutoscalerScraper) enqueueGenericEventsFromMetrics(ctx context.Context) {
	objectList, err := getStorageAutoScalerObjects(ctx, r.Client)
	if err != nil {
		r.Log.Error(err, "failed to get resource objects")
		return
	}
	if len(objectList.Items) == 0 {
		return
	}

	osdUsageQuery := `(ceph_osd_metadata * on (ceph_daemon, namespace, managedBy) group_right(device_class,hostname) (ceph_osd_stat_bytes_used / ceph_osd_stat_bytes))`
	metrics, err := scrapeMetrics(ctx, r.OperatorNamespace, osdUsageQuery, r.Log)
	if err != nil {
		r.Log.Error(err, "failed to scrape metrics")
		return
	}

	// refresh the sync map
	r.SyncMap.Clear()
	// update the sync map with the highest value of osd usage per device class
	updateSyncMap(metrics, r.SyncMap)

	filteredObjectList := filterObjectsForScaling(objectList, r.SyncMap, r.Log)

	// send generic events for the filtered objects
	sendGenericEvent(filteredObjectList, r.EventCh, r.Log)
}

// update the sync map with the highest value of osd usage per device class
func updateSyncMap(metrics model.Vector, syncMap *sync.Map) {
	// create a map of device class to highest osd usage
	deviceClassUsage := make(map[string]float64)
	for _, osd := range metrics {
		deviceClass := string(osd.Metric["device_class"])
		deviceClassUsage[deviceClass] = math.Max(deviceClassUsage[deviceClass], float64(osd.Value))
	}

	// update the sync map
	for deviceClass, usage := range deviceClassUsage {
		syncMap.Store(deviceClass, usage)
	}
}

func getStorageAutoScalerObjects(ctx context.Context, client client.Client) (*ocsv1.StorageAutoScalerList, error) {
	// get all the storage autoscaler cr objects
	storageAutoScalerList := &ocsv1.StorageAutoScalerList{}
	if err := client.List(ctx, storageAutoScalerList); err != nil {
		return nil, err
	}

	return storageAutoScalerList, nil
}

func filterObjectsForScaling(storageAutoScalerList *ocsv1.StorageAutoScalerList, syncMap *sync.Map, log logr.Logger) []types.NamespacedName {
	var objectList []types.NamespacedName

	for _, storageAutoScaler := range storageAutoScalerList.Items {
		deviceClass := storageAutoScaler.Spec.DeviceClass
		deviceClassThreshold := storageAutoScaler.Spec.StorageScalingThresholdPercent
		// get the osd usage from the sync map
		usage, ok := syncMap.Load(deviceClass)
		if !ok {
			log.Error(fmt.Errorf("osd usage not found for device class"), "osd usage not found for device class", "device class", deviceClass)
		} else if usage == nil {
			err := fmt.Errorf("osd usage is nil for device class, device class: %s", storageAutoScaler.Spec.DeviceClass)
			log.Error(err, "device class not found in sync map")
		} else {
			deviceClassUsage := usage.(float64)
			if (deviceClassUsage * 100) >= float64(deviceClassThreshold) {
				objectList = append(objectList, types.NamespacedName{Namespace: storageAutoScaler.Namespace, Name: storageAutoScaler.Name})
			}
		}
	}

	return objectList
}

func sendGenericEvent(objectList []types.NamespacedName, eventCh chan event.GenericEvent, log logr.Logger) {
	// send a generic event to trigger the reconcile
	for _, object := range objectList {
		log.Info("sending generic event", "object", object)
		eventCh <- event.GenericEvent{
			Object: &ocsv1.StorageAutoScaler{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: object.Namespace,
					Name:      object.Name,
				},
			},
		}
	}
}
