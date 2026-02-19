package collectors

import (
	"context"

	"github.com/ceph/go-ceph/cephfs/admin"
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

var _ prometheus.Collector = &CephFSSubvolumeCountCollector{}

// CephFSSubvolumeCountCollector counts subvolumes per SubVolumeGroup
// using go-ceph FSAdmin.
type CephFSSubvolumeCountCollector struct {
	conn            *cephconn.Conn
	rookClient      rookclient.Interface
	namespace       string
	subvolumeCount  *prometheus.Desc
}

func NewCephFSSubvolumeCountCollector(conn *cephconn.Conn, opts *options.Options) *CephFSSubvolumeCountCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client: %v", err)
		return nil
	}

	return &CephFSSubvolumeCountCollector{
		conn:       conn,
		rookClient: client,
		namespace:  opts.AllowedNamespaces[0],
		subvolumeCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "cephfs", "subvolume_count"),
			"Number of CephFS subvolumes in a SubVolumeGroup",
			[]string{"consumer_name"},
			nil,
		),
	}
}

func (c *CephFSSubvolumeCountCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.subvolumeCount
}

// TODO: Remote consumer PV counts need to come via gRPC ReportStatus.
// Internal consumers can use kube_persistentvolume_info in PromQL.

func (c *CephFSSubvolumeCountCollector) Collect(ch chan<- prometheus.Metric) {
	conn, err := c.conn.Get()
	if err != nil {
		klog.Errorf("failed to get ceph connection: %v", err)
		return
	}

	fsa := admin.NewFromConn(conn)

	volumes, err := fsa.ListVolumes()
	if err != nil {
		klog.Errorf("failed to list cephfs volumes: %v", err)
		c.conn.Reconnect()
		return
	}

	groupToConsumer := buildSubVolumeGroupToConsumerMap(c.rookClient, c.namespace)

	for _, volume := range volumes {
		groups, err := fsa.ListSubVolumeGroups(volume)
		if err != nil {
			klog.Errorf("failed to list subvolume groups for volume %s: %v", volume, err)
			continue
		}

		for _, group := range groups {
			subvolumes, err := fsa.ListSubVolumes(volume, group)
			if err != nil {
				klog.Errorf("failed to list subvolumes for volume %s group %s: %v", volume, group, err)
				continue
			}

			consumerName := groupToConsumer[group]

			ch <- prometheus.MustNewConstMetric(c.subvolumeCount,
				prometheus.GaugeValue, float64(len(subvolumes)),
				consumerName,
			)
		}
	}
}

// buildSubVolumeGroupToConsumerMap maps SVG Spec.Name -> StorageConsumer name.
func buildSubVolumeGroupToConsumerMap(client rookclient.Interface, ns string) map[string]string {
	groupToConsumer := make(map[string]string)

	svgList, err := client.CephV1().CephFilesystemSubVolumeGroups(ns).List(
		context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list CephFilesystemSubVolumeGroups: %v", err)
		return groupToConsumer
	}

	for _, svg := range svgList.Items {
		// Spec.Name is the actual Ceph SubVolumeGroup name;
		// falls back to CR name if unset.
		groupName := svg.Spec.Name
		if groupName == "" {
			groupName = svg.Name
		}
		for _, ref := range svg.OwnerReferences {
			if ref.Kind == "StorageConsumer" {
				groupToConsumer[groupName] = ref.Name
				break
			}
		}
	}
	return groupToConsumer
}
