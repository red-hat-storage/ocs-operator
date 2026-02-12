package collectors

import (
	"github.com/prometheus/client_golang/prometheus"
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
	MirrorDaemonHealth *prometheus.Desc
	ImageStatusState   *prometheus.Desc
}

func NewRBDMirrorCollector() *RBDMirrorCollector {
	commonRBDMirrorLabels := []string{"image", "pool_name", "site_name"}
	return &RBDMirrorCollector{
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
	ch <- c.MirrorDaemonHealth
	ch <- c.ImageStatusState
}

// Collect implements prometheus.Collector interface
func (c *RBDMirrorCollector) Collect(ch chan<- prometheus.Metric) {
	// TODO: re-implement without cache, query rbd mirror pool status directly
}
