package collectors

import (
	"context"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const pvMetadataKey = "csi.storage.k8s.io/pv/name"

var _ prometheus.Collector = &CephRBDCollector{}

// CephRBDCollector collects PV metadata, children count, and mirror
// state for RBD images using go-ceph.
type CephRBDCollector struct {
	conn          *cephconn.Conn
	rookClient    rookclient.Interface
	namespace     string
	pvMetadata    *prometheus.Desc
	childrenCount *prometheus.Desc
	mirrorState   *prometheus.Desc
}

func NewCephRBDCollector(conn *cephconn.Conn, opts *options.Options) *CephRBDCollector {
	client, err := rookclient.NewForConfig(opts.Kubeconfig)
	if err != nil {
		klog.Errorf("failed to create rook client: %v", err)
		return nil
	}

	return &CephRBDCollector{
		conn:       conn,
		rookClient: client,
		namespace:  opts.AllowedNamespaces[0],
		pvMetadata: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "pv_metadata"),
			"Attributes of Ceph RBD based Persistent Volume",
			[]string{"name", "image", "pool_name", "radosnamespace", "consumer_name"},
			nil,
		),
		childrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"Number of RBD children (clones) for a PV-backed image",
			[]string{"image", "pool_name", "radosnamespace", "consumer_name"},
			nil,
		),
		mirrorState: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd_mirror", "image_state"),
			"Mirrored image state",
			[]string{"image", "pool_name", "site_name", "consumer_name"},
			nil,
		),
	}
}

func (c *CephRBDCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pvMetadata
	ch <- c.childrenCount
	ch <- c.mirrorState
}

func (c *CephRBDCollector) Collect(ch chan<- prometheus.Metric) {
	conn, err := c.conn.Get()
	if err != nil {
		klog.Errorf("failed to get ceph connection: %v", err)
		return
	}

	pools, err := conn.ListPools()
	if err != nil {
		klog.Errorf("failed to list ceph pools: %v", err)
		c.conn.Reconnect()
		return
	}

	nsToConsumer := buildRadosNamespaceToConsumerMap(c.rookClient, c.namespace)

	for _, pool := range pools {
		ioctx, err := conn.OpenIOContext(pool)
		if err != nil {
			continue
		}

		mirrorMode, _ := rbd.GetMirrorMode(ioctx)
		mirrored := mirrorMode != rbd.MirrorModeDisabled

		var peerMap map[string]string
		if mirrored {
			peerMap = buildMirrorPeerMap(ioctx)
		}

		c.collectPool(ioctx, pool, "", nsToConsumer, mirrored, peerMap, ch)

		namespaces, err := rbd.NamespaceList(ioctx)
		if err != nil {
			ioctx.Destroy()
			continue
		}
		for _, ns := range namespaces {
			ioctx.SetNamespace(ns)
			c.collectPool(ioctx, pool, ns, nsToConsumer, mirrored, peerMap, ch)
		}
		ioctx.Destroy()
	}
}

func (c *CephRBDCollector) collectPool(
	ioctx *rados.IOContext,
	pool, radosNamespace string,
	nsToConsumer map[string]string,
	mirrored bool,
	peerMap map[string]string,
	ch chan<- prometheus.Metric,
) {
	consumerName := nsToConsumer[radosNamespace]

	c.collectImageMetrics(ioctx, pool, radosNamespace, consumerName, ch)
	if mirrored {
		c.collectMirrorStatus(ioctx, pool, radosNamespace, consumerName, peerMap, ch)
	}
}

func (c *CephRBDCollector) collectImageMetrics(
	ioctx *rados.IOContext,
	pool, radosNamespace, consumerName string,
	ch chan<- prometheus.Metric,
) {
	imageNames, err := rbd.GetImageNames(ioctx)
	if err != nil {
		return
	}

	for _, imageName := range imageNames {
		img, err := rbd.OpenImageReadOnly(ioctx, imageName, rbd.NoSnapshot)
		if err != nil {
			klog.Errorf("failed to open rbd image %s/%s: %v", pool, imageName, err)
			continue
		}

		// Not a CSI-provisioned image; skip.
		pvName, err := img.GetMetadata(pvMetadataKey)
		if err != nil {
			img.Close()
			continue
		}

		ch <- prometheus.MustNewConstMetric(c.pvMetadata,
			prometheus.GaugeValue, 1,
			pvName, imageName, pool, radosNamespace, consumerName,
		)

		_, children, err := img.ListChildren()
		img.Close()
		if err != nil {
			klog.Errorf("failed to list children for image %s/%s: %v", pool, imageName, err)
			continue
		}

		ch <- prometheus.MustNewConstMetric(c.childrenCount,
			prometheus.GaugeValue, float64(len(children)),
			imageName, pool, radosNamespace, consumerName,
		)
	}
}

func (c *CephRBDCollector) collectMirrorStatus(
	ioctx *rados.IOContext,
	pool, radosNamespace, consumerName string,
	peerMap map[string]string,
	ch chan<- prometheus.Metric,
) {
	statuses, err := rbd.MirrorImageGlobalStatusList(ioctx, "", 0)
	if err != nil {
		klog.Errorf("failed to list mirror image status for pool %s namespace %q: %v", pool, radosNamespace, err)
		return
	}

	for _, item := range statuses {
		for _, site := range item.Status.SiteStatuses {
			// Local site, not a peer.
			if site.MirrorUUID == "" {
				continue
			}

			siteName := peerMap[site.MirrorUUID]

			ch <- prometheus.MustNewConstMetric(c.mirrorState,
				prometheus.GaugeValue, float64(site.State),
				item.Status.Name, pool, siteName, consumerName,
			)
		}
	}
}

// buildMirrorPeerMap maps peer MirrorUUID -> site name.
func buildMirrorPeerMap(ioctx *rados.IOContext) map[string]string {
	peerMap := make(map[string]string)

	peers, err := rbd.ListMirrorPeerSite(ioctx)
	if err != nil {
		klog.Errorf("failed to list mirror peer sites: %v", err)
		return peerMap
	}

	for _, peer := range peers {
		peerMap[peer.MirrorUUID] = peer.SiteName
	}
	return peerMap
}

func buildRadosNamespaceToConsumerMap(client rookclient.Interface, ns string) map[string]string {
	nsToConsumer := make(map[string]string)

	rnsList, err := client.CephV1().CephBlockPoolRadosNamespaces(ns).List(
		context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list CephBlockPoolRadosNamespaces: %v", err)
		return nsToConsumer
	}

	for _, rns := range rnsList.Items {
		radosNSName := rns.Spec.Name
		if radosNSName == "" {
			radosNSName = rns.Name
		}
		// <implicit> means the default (empty) rados namespace in Ceph
		if radosNSName == "<implicit>" {
			radosNSName = ""
		}
		for _, ref := range rns.OwnerReferences {
			if ref.Kind == "StorageConsumer" {
				nsToConsumer[radosNSName] = ref.Name
				break
			}
		}
	}
	return nsToConsumer
}
