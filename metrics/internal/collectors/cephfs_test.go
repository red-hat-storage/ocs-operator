package collectors

import (
	"strings"
	"sync/atomic"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

func TestCephFSSubvolumeCountCollectorCollect(t *testing.T) {
	c := &CephFSSubvolumeCountCollector{
		pvMetadata: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "pv_metadata"),
			"test", []string{"name", "subvolume", "volume", "subvolume_group", "consumer_name"}, nil,
		),
		subvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"test", []string{"consumer_name"}, nil,
		),
		snapshotContentCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "snapshot_content_count"),
			"test", []string{"consumer_name"}, nil,
		),
	}

	t.Run("nil cache returns no metrics", func(t *testing.T) {
		ch := make(chan prometheus.Metric, 10)
		c.Collect(ch)
		close(ch)
		if len(ch) != 0 {
			t.Errorf("expected 0 metrics, got %d", len(ch))
		}
	})

	t.Run("populated cache emits metrics", func(t *testing.T) {
		snap := &cephfsCacheSnapshot{
			groups: []cephfsGroupData{
				{
					consumerName: "consumer-a",
					volume:       "cephfs-vol",
					group:        "csi",
					subvolumes: map[string]string{
						"sv-001": "pvc-aaa",
						"sv-002": "pvc-bbb",
					},
				},
				{
					consumerName: "consumer-b",
					volume:       "cephfs-vol",
					group:        "csi-2",
					subvolumes: map[string]string{
						"sv-003": "pvc-ccc",
					},
				},
			},
			subvolumesByConsumer: map[string]int{
				"consumer-a": 2,
				"consumer-b": 3,
			},
			snapshotContentsByConsumer: map[string]int{
				"consumer-a": 1,
				"consumer-b": 4,
			},
		}
		c.cache.Store(snap)

		ch := make(chan prometheus.Metric, 20)
		c.Collect(ch)
		close(ch)

		var metrics []prometheus.Metric
		for m := range ch {
			metrics = append(metrics, m)
		}

		// 2 subvolumeCount (per consumer) + 2 snapshotContentCount (per consumer) + 3 pvMetadata = 7
		if len(metrics) != 7 {
			t.Fatalf("expected 7 metrics, got %d", len(metrics))
		}

		subvolumeCountsByConsumer := make(map[string]float64)
		snapshotCountsByConsumer := make(map[string]float64)
		for _, m := range metrics {
			var d dto.Metric
			if err := m.Write(&d); err != nil {
				t.Fatal(err)
			}
			desc := m.Desc().String()
			var consumer string
			for _, lp := range d.Label {
				if lp.GetName() == "consumer_name" {
					consumer = lp.GetValue()
					break
				}
			}
			if consumer == "" || d.Gauge == nil {
				continue
			}
			// Filter by metric descriptor to only count the relevant metrics
			if strings.Contains(desc, "subvolume_count") {
				subvolumeCountsByConsumer[consumer] = d.Gauge.GetValue()
			} else if strings.Contains(desc, "snapshot_content_count") {
				snapshotCountsByConsumer[consumer] = d.Gauge.GetValue()
			}
		}
		if subvolumeCountsByConsumer["consumer-a"] != 2 {
			t.Errorf("consumer-a subvolume_count = %v, want 2", subvolumeCountsByConsumer["consumer-a"])
		}
		if subvolumeCountsByConsumer["consumer-b"] != 3 {
			t.Errorf("consumer-b subvolume_count = %v, want 3", subvolumeCountsByConsumer["consumer-b"])
		}
		if snapshotCountsByConsumer["consumer-a"] != 1 {
			t.Errorf("consumer-a snapshot_content_count = %v, want 1", snapshotCountsByConsumer["consumer-a"])
		}
		if snapshotCountsByConsumer["consumer-b"] != 4 {
			t.Errorf("consumer-b snapshot_content_count = %v, want 4", snapshotCountsByConsumer["consumer-b"])
		}
	})
}

func TestCephFSSubvolumeCountCollectorDescribe(t *testing.T) {
	c := &CephFSSubvolumeCountCollector{
		pvMetadata:           prometheus.NewDesc("a", "a", nil, nil),
		subvolumeCount:       prometheus.NewDesc("b", "b", nil, nil),
		snapshotContentCount: prometheus.NewDesc("c", "c", nil, nil),
	}

	ch := make(chan *prometheus.Desc, 10)
	c.Describe(ch)
	close(ch)

	var descs []*prometheus.Desc
	for d := range ch {
		descs = append(descs, d)
	}
	if len(descs) != 3 {
		t.Errorf("expected 3 descriptors, got %d", len(descs))
	}
}

func TestCephFSCacheAtomicPointer(t *testing.T) {
	var p atomic.Pointer[cephfsCacheSnapshot]
	if p.Load() != nil {
		t.Error("zero value should be nil")
	}
	snap := &cephfsCacheSnapshot{subvolumesByConsumer: map[string]int{"test": 42}}
	p.Store(snap)
	if p.Load().subvolumesByConsumer["test"] != 42 {
		t.Error("stored snapshot should be retrievable")
	}
}
