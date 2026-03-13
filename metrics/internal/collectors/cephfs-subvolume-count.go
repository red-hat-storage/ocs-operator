package collectors

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/ceph/go-ceph/cephfs/admin"
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &CephFSSubvolumeCountCollector{}

type cephfsGroupData struct {
	consumerName string
	volume       string
	group        string
	subvolumes   map[string]string // subvolume name -> PV name
}

type cephfsCacheSnapshot struct {
	groups          []cephfsGroupData
	totalSubvolumes int
}

type CephFSSubvolumeCountCollector struct {
	conn           *cephconn.Conn
	rookClient     rookclient.Interface
	namespace      string
	scanInterval   time.Duration
	pvMetadata     *prometheus.Desc
	subvolumeCount *prometheus.Desc
	cache          atomic.Pointer[cephfsCacheSnapshot]
}

func NewCephFSSubvolumeCountCollector(conn *cephconn.Conn, rookClient rookclient.Interface, ns string, scanInterval time.Duration) *CephFSSubvolumeCountCollector {
	return &CephFSSubvolumeCountCollector{
		conn:         conn,
		rookClient:   rookClient,
		namespace:    ns,
		scanInterval: scanInterval,
		pvMetadata: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "pv_metadata"),
			"Attributes of CephFS based Persistent Volume",
			[]string{"name", "subvolume", "volume", "subvolume_group", "consumer_name"},
			nil,
		),
		subvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"Total number of CephFS subvolumes",
			nil, nil,
		),
	}
}

func (c *CephFSSubvolumeCountCollector) Run(stopCh <-chan struct{}) {
	go runScanLoop(stopCh, c.scanInterval, c.runScan)
}

func (c *CephFSSubvolumeCountCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pvMetadata
	ch <- c.subvolumeCount
}

func (c *CephFSSubvolumeCountCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.cache.Load()
	if snap == nil {
		klog.Warning("CephFS cache not yet populated, skipping")
		return
	}

	ch <- prometheus.MustNewConstMetric(c.subvolumeCount,
		prometheus.GaugeValue, float64(snap.totalSubvolumes))

	for _, g := range snap.groups {
		for svName, pvName := range g.subvolumes {
			ch <- prometheus.MustNewConstMetric(c.pvMetadata,
				prometheus.GaugeValue, 1,
				pvName, svName, g.volume, g.group, g.consumerName,
			)
		}
	}
}

func (c *CephFSSubvolumeCountCollector) runScan() {
	start := time.Now()

	conn, err := c.conn.Get()
	if err != nil {
		klog.Errorf("cephfs scan: failed to get ceph connection: %v", err)
		c.conn.Reconnect()
		return
	}

	fsa := admin.NewFromConn(conn)

	volumes, err := fsa.ListVolumes()
	if err != nil {
		klog.Errorf("cephfs scan: failed to list volumes: %v", err)
		c.conn.Reconnect()
		return
	}

	groupToConsumer := buildSubVolumeGroupToConsumerMap(c.rookClient, c.namespace)

	var groups []cephfsGroupData
	anyVolumeSucceeded := false
	totalSubvolumes := 0
	pvLinkedSubvolumes := 0

	for _, volume := range volumes {
		svGroups, err := fsa.ListSubVolumeGroups(volume)
		if err != nil {
			klog.Errorf("cephfs scan: failed to list subvolume groups for %s: %v", volume, err)
			continue
		}
		anyVolumeSucceeded = true

		for _, group := range svGroups {
			subvolNames, err := fsa.ListSubVolumes(volume, group)
			if err != nil {
				klog.Errorf("cephfs scan: failed to list subvolumes for %s/%s: %v", volume, group, err)
				continue
			}
			totalSubvolumes += len(subvolNames)

			subvolumes := make(map[string]string, len(subvolNames))
			for _, sv := range subvolNames {
				pvName, err := fsa.GetMetadata(volume, group, sv, pvMetadataKey)
				if err != nil {
					klog.V(4).Infof("cephfs scan: no PV metadata for %s/%s/%s: %v", volume, group, sv, err)
					continue
				}
				subvolumes[sv] = pvName
			}

			if len(subvolumes) > 0 {
				groups = append(groups, cephfsGroupData{
					consumerName: groupToConsumer[group],
					volume:       volume,
					group:        group,
					subvolumes:   subvolumes,
				})
				pvLinkedSubvolumes += len(subvolumes)
			}
		}
	}

	if len(volumes) > 0 && !anyVolumeSucceeded {
		klog.Error("cephfs scan: failed for all volumes, reconnecting")
		c.conn.Reconnect()
		return
	}

	c.cache.Store(&cephfsCacheSnapshot{
		groups:          groups,
		totalSubvolumes: totalSubvolumes,
	})

	klog.Infof("cephfs scan: completed in %v, groups=%d, subvolumes=%d (with PV: %d)",
		time.Since(start), len(groups), totalSubvolumes, pvLinkedSubvolumes)
}

func buildSubVolumeGroupToConsumerMap(client rookclient.Interface, ns string) map[string]string {
	groupToConsumer := make(map[string]string)

	svgList, err := client.CephV1().CephFilesystemSubVolumeGroups(ns).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list CephFilesystemSubVolumeGroups: %v", err)
		return groupToConsumer
	}

	for _, svg := range svgList.Items {
		groupName := svg.Spec.Name
		if groupName == "" {
			groupName = svg.Name
		}
		if name := consumerOwnerName(svg.OwnerReferences); name != "" {
			groupToConsumer[groupName] = name
		}
	}
	return groupToConsumer
}
