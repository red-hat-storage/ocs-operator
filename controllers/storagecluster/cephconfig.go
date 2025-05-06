package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"reflect"
	"strings"

	ctrl "sigs.k8s.io/controller-runtime"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ini "gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephConfig struct{}

const (
	rookOverrideConfigMapName = "rook-config-override"
	globalSectionKey          = "global"
	publicNetworkKey          = "public_network"
)

var (
	defaultRookConfig = `
[global]
bdev_flock_retry = 20
mon_osd_full_ratio = .85
mon_osd_backfillfull_ratio = .8
mon_osd_nearfull_ratio = .75
mon_pg_warn_max_object_skew = 0
mon_data_avail_warn = 15
mon_warn_on_pool_no_redundancy = false
bluestore_prefer_deferred_size_hdd = 0
bluestore_slow_ops_warn_lifetime = 0
[osd]
osd_memory_target_cgroup_limit_ratio = 0.8
[client.rbd-mirror.a]
debug_ms = 1
debug_rbd = 15
debug_rbd_mirror = 30
log_file = /var/log/ceph/\$cluster-\$name.log
[client.rbd-mirror-peer]
debug_ms = 1
debug_rbd = 15
debug_rbd_mirror = 30
log_file = /var/log/ceph/\$cluster-\$name.log
`
)

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *ocsCephConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephConfig.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore || reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}
	rookConfig, configErr := getRookCephConfig(r, sc)
	if configErr != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get rook ceph config data: %w", configErr)
	}
	rookConfigOverrideData := map[string]string{
		"config": rookConfig,
	}
	rookConfigOverrideCM := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookOverrideConfigMapName,
			Namespace: sc.Namespace,
		},
		Data: rookConfigOverrideData,
	}
	_, err := ctrl.CreateOrUpdate(context.Background(), r.Client, rookConfigOverrideCM, func() error {
		if !reflect.DeepEqual(rookConfigOverrideCM.Data, rookConfigOverrideData) {
			r.Log.Info("updating rook config override configmap", "ConfigMap", klog.KRef(sc.Namespace, rookOverrideConfigMapName))
			rookConfigOverrideCM.Data = rookConfigOverrideData
		}
		return ctrl.SetControllerReference(sc, rookConfigOverrideCM, r.Scheme)
	})
	if err != nil {
		r.Log.Error(err, "failed to create or update rook config override", "ConfigMap", klog.KRef(sc.Namespace, rookOverrideConfigMapName))
		return reconcile.Result{}, fmt.Errorf("failed to create or update rook config override: %w", err)
	}
	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsCephConfig
func (obj *ocsCephConfig) ensureDeleted(_ *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	return reconcile.Result{}, nil
}

// updateRookConfig(config string, section string, value string )(string, error)
func updateRookConfig(defaultRookConfigData string, section string, key string, val string) (string, error) {
	if defaultRookConfigData == "" {
		return "", fmt.Errorf("failed to update rook config: defaultRookConfigData is empty")
	}

	if val == "" {
		return "", fmt.Errorf("failed to update rook config: value is empty")
	}
	cfg, err := ini.Load([]byte(defaultRookConfigData))
	if err != nil {
		return "", fmt.Errorf("failed to load configData by ini Loader : %v", err)
	}
	cfg.Section(section).Key(key).SetValue(val)
	var b bytes.Buffer
	_, err = cfg.WriteTo(&b)
	if err != nil {
		return "", fmt.Errorf("failed to write to bytes buffer from ini cfg: %v", err)
	}
	return b.String(), nil
}

func getRookCephConfig(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (string, error) {
	rookConfig := defaultRookConfig
	// configure public network if the cluster is dualstack, but not multus
	if sc.Spec.Network != nil && sc.Spec.Network.Provider == "" && sc.Spec.Network.DualStack {
		log.Info("DualStack is enabled, and no alternate network provider is detected")

		networkConfig := &configv1.Network{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "cluster", Namespace: ""}, networkConfig)
		if err != nil {
			return "", fmt.Errorf("could not get network config details : %v", err)
		}
		cidrNameArray := []string{}
		for _, cidr := range networkConfig.Status.ClusterNetwork {
			cidrNameArray = append(cidrNameArray, cidr.CIDR)
		}
		if len(cidrNameArray) == 0 {
			return "", fmt.Errorf("no CIDR is detected")
		}
		cidrName := strings.Join(cidrNameArray, ",")
		rookConfig, err = updateRookConfig(rookConfig, globalSectionKey, publicNetworkKey, cidrName)
		if err != nil {
			return "", fmt.Errorf("failed to set network configuration for rook: %v", err)
		}
	}
	return rookConfig, nil
}
