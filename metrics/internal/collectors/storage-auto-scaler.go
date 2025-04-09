package collectors

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

type PhaseStatus int

const (
	// ResetPhaseStatus is something which comes after a 'SucceededPhaseStatus'
	// and we will stop sending 'AutoScalerPhaseStatus' at this phase
	ResetPhaseStatus PhaseStatus = iota
	NotStartedPhaseStatus
	InProgressPhaseStatus
	SucceededPhaseStatus
	FailedPhaseStatus
)

type StorageAutoScalerCollector struct {
	AutoScalerPhaseStatus  *prometheus.Desc
	StorageCapacityReached *prometheus.Desc
	Informer               cache.SharedIndexInformer
}

var _ prometheus.Collector = &StorageAutoScalerCollector{}

func (s *StorageAutoScalerCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		s.AutoScalerPhaseStatus,
		s.StorageCapacityReached,
	}
	for _, d := range ds {
		ch <- d
	}
}

func (s *StorageAutoScalerCollector) Collect(ch chan<- prometheus.Metric) {
	sasLister := NewStorageAutoScalerLister(s.Informer.GetIndexer())
	storageAutoScalers, err := sasLister.List(labels.Everything())
	if err != nil {
		klog.Errorf("couldn't list StorageAutoScaler elements: %v", err)
		return
	} else if len(storageAutoScalers) == 0 {
		return
	}
	var wg sync.WaitGroup
	wg.Add(2) // we are waiting for TWO go routines to finish
	go s.collectStorageAutoScalerPhaseStatuses(ch, storageAutoScalers, &wg)
	go s.collectStorageCapacityReachedStatuses(ch, storageAutoScalers, &wg)
	wg.Wait()
}

func (s *StorageAutoScalerCollector) Run(stopCh <-chan struct{}) {
	go s.Informer.Run(stopCh)
}

func (s *StorageAutoScalerCollector) collectStorageAutoScalerPhaseStatuses(ch chan<- prometheus.Metric, storageAutoScalers []*v1.StorageAutoScaler, wg *sync.WaitGroup) {
	defer wg.Done()
	for _, storageAutoScaler := range storageAutoScalers {
		var phaseStatus = NotStartedPhaseStatus
		switch storageAutoScaler.Status.Phase {
		case v1.StorageAutoScalerPhaseFailed:
			phaseStatus = FailedPhaseStatus
		case v1.StorageAutoScalerPhaseSucceeded:
			phaseStatus = succeededPhaseValidation(storageAutoScaler.Status.LastExpansion.CompletionTime)
		case v1.StorageAutoScalerPhaseInProgress:
			phaseStatus = InProgressPhaseStatus
		default:
			phaseStatus = NotStartedPhaseStatus
		}
		// send metric on any phase other than 'Reset' phase
		if phaseStatus != ResetPhaseStatus {
			ch <- prometheus.MustNewConstMetric(
				s.AutoScalerPhaseStatus,
				prometheus.GaugeValue,
				float64(phaseStatus),
				storageAutoScaler.Name,
				storageAutoScaler.Namespace,
				storageAutoScaler.Spec.StorageCluster.Name,
				storageAutoScaler.Spec.DeviceClass,
			)
		}
	}
}

func succeededPhaseValidation(completionTime metav1.Time) PhaseStatus {
	currentTime := metav1.Now().Time
	var zeroValuedTime metav1.Time
	// if completion time is not set
	// OR
	// success happened within a 10mins time window
	// we return success
	if completionTime.Sub(zeroValuedTime.Time) == 0 ||
		currentTime.Sub(completionTime.Time) < 10*time.Minute {
		return SucceededPhaseStatus
	}
	// after 10 mins, we will reset the success phase
	return ResetPhaseStatus
}

func (s *StorageAutoScalerCollector) collectStorageCapacityReachedStatuses(ch chan<- prometheus.Metric, storageAutoScalers []*v1.StorageAutoScaler, wg *sync.WaitGroup) {
	defer wg.Done()
	for _, storageAutoScaler := range storageAutoScalers {
		var capacityReached = 0
		if storageAutoScaler.Status.StorageCapacityLimitReached != nil && *storageAutoScaler.Status.StorageCapacityLimitReached {
			capacityReached = 1
		}
		ch <- prometheus.MustNewConstMetric(
			s.StorageCapacityReached,
			prometheus.GaugeValue,
			float64(capacityReached),
			storageAutoScaler.Name,
			storageAutoScaler.Namespace,
			storageAutoScaler.Spec.StorageCluster.Name,
			storageAutoScaler.Spec.DeviceClass,
		)
	}
}

func NewStorageAutoScalerCollector(opts *options.Options) *StorageAutoScalerCollector {
	c, err := GetOcsV1Client(opts)
	if err != nil {
		klog.Errorf("Unable to get client: %v", err)
		return nil
	}
	lw := cache.NewListWatchFromClient(c, "storageautoscalers", searchInNamespace(opts), fields.Everything())
	sharedIndexInformer := cache.NewSharedIndexInformer(lw, &v1.StorageAutoScaler{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	return &StorageAutoScalerCollector{
		AutoScalerPhaseStatus: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "storageautoscaler", "phase_status"),
			fmt.Sprintf("Phases are; Not Started: %d, In Progress: %d, Succeessfull: %d, Failed: %d", NotStartedPhaseStatus, InProgressPhaseStatus, SucceededPhaseStatus, FailedPhaseStatus),
			[]string{"name", "namespace", "storagecluster", "device_class"},
			nil,
		),
		StorageCapacityReached: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "storageautoscaler", "capacity_reached"),
			`Will be set to ONE if storage capacity limit is reached, otherwise ZERO`,
			[]string{"name", "namespace", "storagecluster", "device_class"},
			nil,
		),
		Informer: sharedIndexInformer,
	}
}
