package collectors

import (
	"context"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/ceph/go-ceph/rados"
	"github.com/ceph/go-ceph/rbd"
	"github.com/prometheus/client_golang/prometheus"
	cephconn "github.com/red-hat-storage/ocs-operator/metrics/v4/internal/ceph"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	"golang.org/x/sync/errgroup"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

const (
	defaultImageWorkers = 8
	imageChunkSize      = 100
)

var _ prometheus.Collector = &CephRBDCollector{}

type rbdImageData struct {
	pvName   string
	children int
}

type rbdMirrorData struct {
	imageName string
	siteName  string
	state     float64
}

type poolNsKey struct {
	pool           string
	radosNamespace string
}

type rbdPoolData struct {
	consumerName string
	images       map[string]rbdImageData
	mirrors      []rbdMirrorData
}

type rbdCacheSnapshot struct {
	pools map[poolNsKey]*rbdPoolData
}

type imageWork struct {
	pool           string
	radosNamespace string
	images         []string
}

type CephRBDCollector struct {
	conn         *cephconn.Conn
	rookClient   rookclient.Interface
	namespace    string
	scanInterval time.Duration

	pvMetadata    *prometheus.Desc
	childrenCount *prometheus.Desc
	mirrorState   *prometheus.Desc

	cache atomic.Pointer[rbdCacheSnapshot]
}

func NewCephRBDCollector(conn *cephconn.Conn, rookClient rookclient.Interface, ns string, scanInterval time.Duration) *CephRBDCollector {
	return &CephRBDCollector{
		conn:         conn,
		rookClient:   rookClient,
		namespace:    ns,
		scanInterval: scanInterval,
		pvMetadata: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "pv_metadata"),
			"Attributes of Ceph RBD based Persistent Volume",
			[]string{"name", "image", "pool_name", "rados_namespace", "consumer_name"},
			nil,
		),
		childrenCount: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, "rbd", "children_count"),
			"Number of RBD children (clones) for a PV-backed image",
			[]string{"image", "pool_name", "rados_namespace", "consumer_name"},
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

func (c *CephRBDCollector) Run(stopCh <-chan struct{}) {
	go runScanLoop(stopCh, c.scanInterval, c.runScan)
}

func (c *CephRBDCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.pvMetadata
	ch <- c.childrenCount
	ch <- c.mirrorState
}

func (c *CephRBDCollector) Collect(ch chan<- prometheus.Metric) {
	snap := c.cache.Load()
	if snap == nil {
		klog.Warning("RBD cache not yet populated, skipping")
		return
	}

	for key, poolData := range snap.pools {
		for imageName, img := range poolData.images {
			ch <- prometheus.MustNewConstMetric(c.pvMetadata,
				prometheus.GaugeValue, 1,
				img.pvName, imageName, key.pool, key.radosNamespace, poolData.consumerName,
			)
			ch <- prometheus.MustNewConstMetric(c.childrenCount,
				prometheus.GaugeValue, float64(img.children),
				imageName, key.pool, key.radosNamespace, poolData.consumerName,
			)
		}
		for _, m := range poolData.mirrors {
			ch <- prometheus.MustNewConstMetric(c.mirrorState,
				prometheus.GaugeValue, m.state,
				m.imageName, key.pool, m.siteName, poolData.consumerName,
			)
		}
	}
}

func (c *CephRBDCollector) runScan() {
	start := time.Now()

	newPools, work, err := c.enumeratePools()
	if err != nil {
		klog.Errorf("rbd scan: %v", err)
		return
	}

	c.processWork(newPools, work)
	c.cache.Store(&rbdCacheSnapshot{pools: newPools})

	totalImages := 0
	for _, poolData := range newPools {
		totalImages += len(poolData.images)
	}
	klog.Infof("rbd scan: completed in %v, images=%d", time.Since(start), totalImages)
}

func (c *CephRBDCollector) enumeratePools() (map[poolNsKey]*rbdPoolData, []imageWork, error) {
	conn, err := c.conn.Get()
	if err != nil {
		c.conn.Reconnect()
		return nil, nil, fmt.Errorf("failed to get ceph connection: %w", err)
	}

	pools, err := conn.ListPools()
	if err != nil {
		c.conn.Reconnect()
		return nil, nil, fmt.Errorf("failed to list pools: %w", err)
	}

	nsToConsumer := buildRadosNamespaceToConsumerMap(c.rookClient, c.namespace)
	newPools := make(map[poolNsKey]*rbdPoolData)
	var work []imageWork

	anyPoolSucceeded := false
	for _, pool := range pools {
		poolWork, err := c.scanPool(conn, pool, nsToConsumer, newPools)
		if err != nil {
			klog.Errorf("rbd scan: %v", err)
			continue
		}
		anyPoolSucceeded = true
		work = append(work, poolWork...)
	}

	if len(pools) > 0 && !anyPoolSucceeded {
		c.conn.Reconnect()
		return nil, nil, fmt.Errorf("failed to open IO context for any pool, reconnecting")
	}

	return newPools, work, nil
}

// scanPool enumerates images and mirror state for a single pool and its rados namespaces.
// The IOContext is created and destroyed within this function, preventing leaks.
func (c *CephRBDCollector) scanPool(
	conn *rados.Conn,
	pool string,
	nsToConsumer map[string]string,
	newPools map[poolNsKey]*rbdPoolData,
) ([]imageWork, error) {
	ioctx, err := conn.OpenIOContext(pool)
	if err != nil {
		return nil, fmt.Errorf("failed to open IO context for pool %s: %w", pool, err)
	}
	defer ioctx.Destroy()

	var work []imageWork

	ioctx.SetNamespace("")
	if poolData, chunks := c.enumerateNamespace(ioctx, pool, "", nsToConsumer); poolData != nil {
		newPools[poolNsKey{pool, ""}] = poolData
		work = append(work, chunks...)
	}

	namespaces, err := rbd.NamespaceList(ioctx)
	if err != nil {
		klog.Errorf("rbd scan: failed to list namespaces for pool %s: %v", pool, err)
		return work, nil
	}
	for _, ns := range namespaces {
		if _, ok := nsToConsumer[ns]; !ok {
			continue
		}
		ioctx.SetNamespace(ns)
		if poolData, chunks := c.enumerateNamespace(ioctx, pool, ns, nsToConsumer); poolData != nil {
			newPools[poolNsKey{pool, ns}] = poolData
			work = append(work, chunks...)
		}
	}

	return work, nil
}

func (c *CephRBDCollector) processWork(newPools map[poolNsKey]*rbdPoolData, work []imageWork) {
	if len(work) == 0 {
		return
	}

	results := make([]map[string]rbdImageData, len(work))
	group := new(errgroup.Group)
	group.SetLimit(defaultImageWorkers)

	for i, chunk := range work {
		group.Go(func() error {
			results[i] = c.processImageChunk(chunk)
			return nil
		})
	}
	_ = group.Wait()

	for i, result := range results {
		if result == nil {
			continue
		}
		key := poolNsKey{work[i].pool, work[i].radosNamespace}
		poolData := newPools[key]
		for name, data := range result {
			poolData.images[name] = data
		}
	}
}

func (c *CephRBDCollector) enumerateNamespace(
	ioctx *rados.IOContext,
	pool, radosNamespace string,
	nsToConsumer map[string]string,
) (*rbdPoolData, []imageWork) {
	images, err := rbd.GetImageNames(ioctx)
	if err != nil {
		klog.V(4).Infof("rbd scan: no images for %s/%q: %v", pool, radosNamespace, err)
		return nil, nil
	}

	poolData := &rbdPoolData{
		consumerName: nsToConsumer[radosNamespace],
		images:       make(map[string]rbdImageData, len(images)),
		mirrors:      collectMirrorData(ioctx, pool, radosNamespace),
	}

	chunks := chunkImages(pool, radosNamespace, images)
	return poolData, chunks
}

// collectMirrorData gathers per-image mirror state for a pool/namespace.
// Returns nil if mirroring is disabled or on any error.
func collectMirrorData(ioctx *rados.IOContext, pool, radosNamespace string) []rbdMirrorData {
	mirrorMode, err := rbd.GetMirrorMode(ioctx)
	if err != nil {
		klog.Warningf("rbd scan: failed to get mirror mode for %s/%q: %v", pool, radosNamespace, err)
		return nil
	}
	if mirrorMode == rbd.MirrorModeDisabled {
		return nil
	}

	peerMap, err := cephconn.BuildMirrorPeerMap(ioctx)
	if err != nil {
		klog.Errorf("rbd scan: skipping mirror metrics for %s: %v", pool, err)
		return nil
	}

	statuses, err := rbd.MirrorImageGlobalStatusList(ioctx, "", 0)
	if err != nil {
		klog.Errorf("rbd scan: failed to list mirror status for %s/%q: %v", pool, radosNamespace, err)
		return nil
	}

	var mirrors []rbdMirrorData
	for _, item := range statuses {
		for _, site := range item.Status.SiteStatuses {
			if site.MirrorUUID == "" {
				continue
			}
			mirrors = append(mirrors, rbdMirrorData{
				imageName: item.Status.Name,
				siteName:  peerMap[site.MirrorUUID],
				state:     float64(site.State),
			})
		}
	}
	return mirrors
}

func chunkImages(pool, radosNamespace string, images []string) []imageWork {
	var chunks []imageWork
	for i := 0; i < len(images); i += imageChunkSize {
		end := i + imageChunkSize
		if end > len(images) {
			end = len(images)
		}
		chunks = append(chunks, imageWork{
			pool:           pool,
			radosNamespace: radosNamespace,
			images:         images[i:end],
		})
	}
	return chunks
}

func processImage(ioctx *rados.IOContext, name string) (rbdImageData, bool) {
	img, err := rbd.OpenImageReadOnly(ioctx, name, rbd.NoSnapshot)
	if err != nil {
		klog.V(4).Infof("rbd worker: failed to open image %s: %v", name, err)
		return rbdImageData{}, false
	}
	defer img.Close()

	pvName, err := img.GetMetadata(pvMetadataKey)
	if err != nil {
		if !errors.Is(err, rbd.ErrNotFound) {
			klog.V(4).Infof("rbd worker: failed to get metadata for %s: %v", name, err)
		}
		return rbdImageData{}, false
	}

	children := 0
	snaps, err := img.GetSnapshotNames()
	if err != nil {
		klog.V(4).Infof("rbd worker: failed to get snapshots for %s: %v", name, err)
	} else if len(snaps) > 0 {
		_, childImages, err := img.ListChildren()
		if err != nil {
			klog.V(4).Infof("rbd worker: failed to list children for %s: %v", name, err)
		} else {
			children = len(childImages)
		}
	}

	return rbdImageData{pvName: pvName, children: children}, true
}

func (c *CephRBDCollector) processImageChunk(w imageWork) map[string]rbdImageData {
	ioctx, err := c.conn.IOContext(w.pool, w.radosNamespace)
	if err != nil {
		klog.Errorf("rbd worker: failed to get IO context for %s/%q: %v", w.pool, w.radosNamespace, err)
		return nil
	}
	defer ioctx.Destroy()

	results := make(map[string]rbdImageData, len(w.images))
	for _, name := range w.images {
		if data, ok := processImage(ioctx, name); ok {
			results[name] = data
		}
	}
	return results
}

func buildRadosNamespaceToConsumerMap(client rookclient.Interface, ns string) map[string]string {
	nsToConsumer := make(map[string]string)

	rnsList, err := client.CephV1().CephBlockPoolRadosNamespaces(ns).List(
		context.Background(), metav1.ListOptions{})
	if err != nil {
		klog.Errorf("failed to list CephBlockPoolRadosNamespaces: %v", err)
		return nsToConsumer
	}

	for _, rns := range rnsList.Items {
		radosNSName := rns.Spec.Name
		if radosNSName == "" {
			radosNSName = rns.Name
		}
		if radosNSName == util.ImplicitRbdRadosNamespaceName {
			radosNSName = ""
		}
		if name := consumerOwnerName(rns.OwnerReferences); name != "" {
			nsToConsumer[radosNSName] = name
		}
	}
	return nsToConsumer
}
