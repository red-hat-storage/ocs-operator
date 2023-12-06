package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ini "gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephConfig struct{}

const (
	rookConfigMapName          = "rook-config-override"
	globalSectionKey           = "global"
	publicNetworkKey           = "public_network"
	warningOnPoolRedundancyKey = "mon_warn_on_pool_no_redundancy"
)

var (
	defaultRookConfigData = `
[global]
bdev_flock_retry = 20
mon_osd_full_ratio = .85
mon_osd_backfillfull_ratio = .8
mon_osd_nearfull_ratio = .75
mon_max_pg_per_osd = 600
mon_pg_warn_max_object_skew = 0
mon_data_avail_warn = 15
bluestore_prefer_deferred_size_hdd = 0
[osd]
osd_memory_target_cgroup_limit_ratio = 0.8
`
)

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *ocsCephConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephConfig.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}
	found := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookConfigMapName, Namespace: sc.Namespace}, found)
	if err == nil && reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	} else if err != nil && !errors.IsNotFound(err) {
		return reconcile.Result{}, err
	}

	ownerRef := metav1.OwnerReference{
		UID:        sc.UID,
		APIVersion: sc.APIVersion,
		Kind:       sc.Kind,
		Name:       sc.Name,
	}
	rookConfigData, configErr := getRookCephConfig(r, sc)
	if configErr != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get rook ceph config data: %w", err)
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rookConfigMapName,
			Namespace:       sc.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Data: map[string]string{
			"config": rookConfigData,
		},
	}

	if err != nil {
		r.Log.Info("Creating Ceph ConfigMap.", "ConfigMap", klog.KRef(sc.Namespace, rookConfigMapName))
		err = r.Client.Create(context.TODO(), cm)
		if err != nil {
			r.Log.Error(err, "Failed to create Ceph ConfigMap.", "ConfigMap", klog.KRef(sc.Namespace, rookConfigMapName))
			return reconcile.Result{}, err
		}
		return reconcile.Result{}, err
	}

	ownerRefFound := false
	for _, ownerRef := range found.OwnerReferences {
		if ownerRef.UID == sc.UID {
			ownerRefFound = true
		}
	}
	val, ok := found.Data["config"]
	if !ok || val != defaultRookConfigData || !ownerRefFound {
		r.Log.Info("Updating Ceph ConfigMap.", "ConfigMap", klog.KRef(sc.Namespace, cm.Name))
		return reconcile.Result{}, r.Client.Update(context.TODO(), cm)
	}
	return reconcile.Result{}, nil
}

// ensureDeleted is dummy func for the ocsCephConfig
func (obj *ocsCephConfig) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
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
	rookConfigData := defaultRookConfigData
	// if Non-Resilient pools are there then suppress the warning for pool no redundancy
	if sc.Spec.ManagedResources.CephNonResilientPools.Enable {
		var err error
		rookConfigData, err = updateRookConfig(rookConfigData, globalSectionKey, warningOnPoolRedundancyKey, "false")
		if err != nil {
			return "", fmt.Errorf("failed to set no warning on no redundancy pool for rook config: %v", err)
		}
		log.Info("Health warning on pool no redundancy is suppressed now as CephNonResilientPools are enabled")
	}
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
		rookConfigData, err = updateRookConfig(rookConfigData, globalSectionKey, publicNetworkKey, cidrName)
		if err != nil {
			return "", fmt.Errorf("failed to set network configuration for rook: %v", err)
		}
	}
	return rookConfigData, nil
}
