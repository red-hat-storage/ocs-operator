package collectors

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	internalcache "github.com/red-hat-storage/ocs-operator/metrics/internal/cache"
	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

const (
	// name of the project/exporter
	namespace      = "ocs"
	cephConfigRoot = "/etc/ceph"
	cephConfigPath = "/etc/ceph/ceph.conf"
	keyRing        = "/etc/ceph/keyring"
)

var cephConfig = []byte(`[global]
auth_cluster_required = cephx
auth_service_required = cephx
auth_client_required = cephx
`)

// RegisterCustomResourceCollectors registers the custom resource collectors
// in the given prometheus.Registry
// This is used to expose metrics about the Custom Resources
func RegisterCustomResourceCollectors(registry *prometheus.Registry, opts *options.Options) {
	cephObjectStoreCollector := NewCephObjectStoreCollector(opts)
	cephBlockPoolCollector := NewCephBlockPoolCollector(opts)
	cephClusterCollector := NewCephClusterCollector(opts)
	OBMetricsCollector := NewObjectBucketCollector(opts)
	cephObjectStoreCollector.Run(opts.StopCh)
	cephBlockPoolCollector.Run(opts.StopCh)
	cephClusterCollector.Run(opts.StopCh)
	OBMetricsCollector.Run(opts.StopCh)
	registry.MustRegister(
		cephObjectStoreCollector,
		cephBlockPoolCollector,
		cephClusterCollector,
		OBMetricsCollector,
	)
}

var pvStoreEnabled bool
var pvStore = internalcache.NewPersistentVolumeStore()

func enablePVStore(opts *options.Options) {
	client := clientset.NewForConfigOrDie(opts.Kubeconfig)
	lw := internalcache.CreatePersistentVolumeListWatch(client, "")
	reflector := cache.NewReflector(lw, &corev1.PersistentVolume{}, pvStore, 0)
	go reflector.Run(opts.StopCh)
	pvStoreEnabled = true
}

var rbdMirrorStoreEnabled bool
var rbdMirrorStore = internalcache.NewRBDMirrorStore()

type csiClusterConfig struct {
	ClusterID string   `json:"clusterID"`
	Monitors  []string `json:"monitors"`
}

func enableRBDMirrorStore(opts *options.Options) error {
	client := clientset.NewForConfigOrDie(opts.Kubeconfig)
	secret, err := client.CoreV1().Secrets("openshift-storage").Get(context.TODO(), "rook-ceph-mon", v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get secret: %v", err)
	}
	key, ok := secret.Data["ceph-secret"]
	if !ok {
		return fmt.Errorf("failed to get client key from secret")
	}
	id := "admin"

	configmap, err := client.CoreV1().ConfigMaps("openshift-storage").Get(context.TODO(), "rook-ceph-csi-config", v1.GetOptions{})
	if err != nil {
		return fmt.Errorf("failed to get configmap: %v", err)
	}

	data := configmap.Data["csi-cluster-config-json"]
	var clusterConfig []csiClusterConfig
	err = json.Unmarshal([]byte(data), &clusterConfig)
	if err != nil {
		return fmt.Errorf("failed to unmarshal csi-cluster-config-json: %v", err)
	}

	// write Ceph config file before issuing RBD mirror commands
	err = writeCephConfig()
	if err != nil {
		return err
	}

	rbdMirrorStore.WithRBDCommandInput(clusterConfig[0].Monitors[0], id, string(key))

	rookClient := rookclient.NewForConfigOrDie(opts.Kubeconfig)
	lw := internalcache.CreateCephBlockPoolListWatch(rookClient, corev1.NamespaceAll, "")
	reflector := cache.NewReflector(lw, &cephv1.CephBlockPool{}, rbdMirrorStore, 30*time.Second)
	go reflector.Run(opts.StopCh)
	rbdMirrorStoreEnabled = true

	return nil
}

// RegisterPersistentVolumeAttributesCollector registers PV attribute colletor to registry
func RegisterPersistentVolumeAttributesCollector(registry *prometheus.Registry, opts *options.Options) {
	if !pvStoreEnabled {
		enablePVStore(opts)
	}
	pvAttributesCollector := NewPersistentVolumeAttributesCollector(pvStore, opts)
	registry.MustRegister(pvAttributesCollector)
}

// RegisterRBDMirrorCollector registers RBD mirror metrics collector to registry
func RegisterRBDMirrorCollector(registry *prometheus.Registry, opts *options.Options) {
	if !pvStoreEnabled {
		enablePVStore(opts)
	}
	if !rbdMirrorStoreEnabled {
		err := enableRBDMirrorStore(opts)
		if err != nil {
			klog.Error(err)
		}
	}
	rbdMirrorCollector := NewRBDMirrorCollector(rbdMirrorStore, pvStore, opts)
	registry.MustRegister(rbdMirrorCollector)
}

/*
	Copied from https://github.com/ceph/ceph-csi/blob/70fc6db2cfe3f00945c030f0d7f83ea1e2d21a00/internal/util/cephconf.go
	Functions to create ceph.conf and keyring files internally.
*/

func createCephConfigRoot() error {
	return os.MkdirAll(cephConfigRoot, 0o755)
}

// createKeyRingFile creates the keyring files to fix above error message logging.
func createKeyRingFile() error {
	var err error
	if _, err = os.Stat(keyRing); os.IsNotExist(err) {
		_, err = os.Create(keyRing)
	}

	return err
}

// writeCephConfig writes out a basic ceph.conf file, making it easy to use
// ceph related CLIs.
func writeCephConfig() error {
	var err error
	if err = createCephConfigRoot(); err != nil {
		return err
	}

	if _, err = os.Stat(cephConfigPath); os.IsNotExist(err) {
		err = os.WriteFile(cephConfigPath, cephConfig, 0o600)
	}

	if err != nil {
		return err
	}

	return createKeyRingFile()
}
