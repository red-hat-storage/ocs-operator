package collectors

import (
	"strings"

	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/v4/metrics/internal/cache"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	"k8s.io/klog/v2"
)

const (
	rbdMirrorSubsystem = "rbd_mirror"
)

const (
	mirrorImageStatusStateUnknown = iota
	mirrorImageStatusStateError
	mirrorImageStatusStateSyncing
	mirrorImageStatusStateStartingReplay
	mirrorImageStatusStateReplaying
	mirrorImageStatusStateStoppingReplay
	mirrorImageStatusStateStopped
)

var _ prometheus.Collector = &RBDMirrorCollector{}

type RBDMirrorCollector struct {
	RBDMirrorStore        *internalcache.RBDMirrorStore
	PersistentVolumeStore *internalcache.PersistentVolumeStore
	// Metric descriptors
	MirrorDaemonHealth *prometheus.Desc
	ImageStatusState   *prometheus.Desc
}

func NewRBDMirrorCollector(mirrorStore *internalcache.RBDMirrorStore, pvStore *internalcache.PersistentVolumeStore, opts *options.Options) *RBDMirrorCollector {
	commonRBDMirrorLabels := []string{"image", "pool_name", "site_name"}
	return &RBDMirrorCollector{
		RBDMirrorStore:        mirrorStore,
		PersistentVolumeStore: pvStore,
		// Metric descriptors
		MirrorDaemonHealth: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, rbdMirrorSubsystem, "daemon_health"),
			"Health of local RBD mirror daemon. 0 = OK, 1 = ERROR",
			[]string{"daemon_id", "hostname"},
			nil,
		),
		ImageStatusState: prometheus.NewDesc(
			prometheus.BuildFQName(namespace, rbdMirrorSubsystem, "image_state"),
			`Mirrored Image State`,
			commonRBDMirrorLabels,
			nil,
		),
	}
}

// Describe implements prometheus.Collector interface
func (c *RBDMirrorCollector) Describe(ch chan<- *prometheus.Desc) {
	ds := []*prometheus.Desc{
		c.MirrorDaemonHealth,
		c.ImageStatusState,
	}

	for _, d := range ds {
		ch <- d
	}
}

// Collect implements prometheus.Collector interface
func (c *RBDMirrorCollector) Collect(ch chan<- prometheus.Metric) {
	c.RBDMirrorStore.Mutex.RLock()
	defer c.RBDMirrorStore.Mutex.RUnlock()

	for _, poolData := range c.RBDMirrorStore.Store {
		for _, daemon := range poolData.MirrorStatus.Daemons {
			switch daemon.Health {
			case "OK":
				ch <- prometheus.MustNewConstMetric(
					c.MirrorDaemonHealth, prometheus.GaugeValue, 0, daemon.ClientID, daemon.Hostname,
				)
			default:
				ch <- prometheus.MustNewConstMetric(
					c.MirrorDaemonHealth, prometheus.GaugeValue, 1, daemon.ClientID, daemon.Hostname,
				)
			}
		}
		for _, image := range poolData.MirrorStatus.Images {
			for _, site := range image.PeerSites {
				// site.State can have values like up+stopped, up+error etc.
				state := strings.SplitN(site.State, "+", 2)
				if len(state) != 2 {
					klog.Errorf("Unexpected mirror state %q of image %q to site %q.", site.State, image.Name, site.SiteName)
				} else {
					switch state[1] {
					case "unknown":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateUnknown,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "error":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateError,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "syncing":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateSyncing,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "starting_replay":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateStartingReplay,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "replaying":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateReplaying,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "stopping_replay":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateStoppingReplay,
							image.Name, poolData.PoolName, site.SiteName,
						)
					case "stopped":
						ch <- prometheus.MustNewConstMetric(
							c.ImageStatusState, prometheus.GaugeValue, mirrorImageStatusStateStopped,
							image.Name, poolData.PoolName, site.SiteName,
						)
					default:
						klog.Errorf("Unknown state %q of image %q", site.State, image.Name)
					}
				}
			}
		}
	}
}
