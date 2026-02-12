package collectors

import (
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"k8s.io/klog/v2"
)

const (
	pvMetadataKey  = "csi.storage.k8s.io/pv/name"
	defaultRBDPool = "ocs-storagecluster-cephblockpool"
)

var _ prometheus.Collector = &PersistentVolumeAttributesCollector{}

type PersistentVolumeAttributesCollector struct {
	conn       *cephconn.Conn
	poolName   string
	PVMetadata *prometheus.Desc
}

func NewPersistentVolumeAttributesCollector(conn *cephconn.Conn) *PersistentVolumeAttributesCollector {
	return &PersistentVolumeAttributesCollector{
		conn:     conn,
		poolName: defaultRBDPool,
		PVMetadata: prometheus.NewDesc(
			prometheus.BuildFQName("ocs", "rbd", "pv_metadata"),
			`Attributes of Ceph RBD based Persistent Volume`,
			[]string{"name", "image", "pool_name"},
			nil,
		),
	}
}

// Describe implements prometheus.Collector interface
func (c *PersistentVolumeAttributesCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.PVMetadata
}

// Collect implements prometheus.Collector interface
func (c *PersistentVolumeAttributesCollector) Collect(ch chan<- prometheus.Metric) {
	conn, err := c.conn.Get()
	if err != nil {
		klog.Errorf("failed to get ceph connection: %v", err)
		return
	}

	ioctx, err := conn.OpenIOContext(c.poolName)
	if err != nil {
		klog.Errorf("failed to open pool %s: %v", c.poolName, err)
		c.conn.Reconnect()
		return
	}
	defer ioctx.Destroy()

	imageNames, err := rbd.GetImageNames(ioctx)
	if err != nil {
		klog.Errorf("failed to list rbd images in pool %s: %v", c.poolName, err)
		return
	}

	for _, imageName := range imageNames {
		img, err := rbd.OpenImageReadOnly(ioctx, imageName, rbd.NoSnapshot)
		if err != nil {
			klog.Errorf("failed to open rbd image %s: %v", imageName, err)
			continue
		}

		pvName, err := img.GetMetadata(pvMetadataKey)
		if err != nil {
			img.Close()
			continue
		}

		img.Close()

		ch <- prometheus.MustNewConstMetric(c.PVMetadata,
			prometheus.GaugeValue, 1,
			pvName, imageName, c.poolName,
		)
	}
}
