package util

import (
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
)

const (
	// EventReasonValidationFailed is used when the StorageCluster spec validation fails
	EventReasonValidationFailed = "FailedValidation"

	// EventReasonUninstallPending is used when the StorageCluster uninstall is Pending
	EventReasonUninstallPending = "UninstallPending"
)

// EventReporter is custom events reporter type which allows user to limit the events
type EventReporter struct {
	recorder record.EventRecorder

	// lastReportedEvent will have a last captured event
	lastReportedEvent map[string]string

	// lastReportedEventTime will be the time of lastReportedEvent
	lastReportedEventTime map[string]time.Time
}

// NewEventReporter returns EventReporter object
func NewEventReporter(recorder record.EventRecorder) *EventReporter {
	return &EventReporter{
		recorder:              recorder,
		lastReportedEvent:     make(map[string]string),
		lastReportedEventTime: make(map[string]time.Time),
	}
}

// ReportIfNotPresent will report event if lastReportedEvent is not the same in last 60 minutes
func (rep *EventReporter) ReportIfNotPresent(instance runtime.Object, eventType, eventReason, msg string) {

	nameSpacedName, err := getNameSpacedName(instance)
	if err != nil {
		return
	}

	eventKey := getEventKey(eventType, eventReason, msg)

	if rep.lastReportedEvent[nameSpacedName] != eventKey || rep.lastReportedEventTime[nameSpacedName].Add(time.Minute*60).Before(time.Now()) {
		rep.lastReportedEvent[nameSpacedName] = eventKey
		rep.lastReportedEventTime[nameSpacedName] = time.Now()
		rep.recorder.Event(instance, eventType, eventReason, msg)
	}
}

func getNameSpacedName(instance runtime.Object) (string, error) {
	objMeta, err := meta.Accessor(instance)
	if err != nil {
		return "", err
	}
	return objMeta.GetNamespace() + ":" + objMeta.GetName(), nil
}

func getEventKey(eventType, eventReason, msg string) string {
	return fmt.Sprintf("%s:%s:%s", eventType, eventReason, msg)
}
