package storagecluster

import (
	"bytes"
	"context"
	"fmt"
	"strings"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ini "gopkg.in/ini.v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type ocsCephConfig struct{}

const (
	rookConfigMapName = "rook-config-override"
	globalSectionKey  = "global"
	publicNetworkKey  = "public_network"
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
[osd]
osd_memory_target_cgroup_limit_ratio = 0.5
`
)

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *ocsCephConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephConfig.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}
	found := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookConfigMapName, Namespace: sc.Namespace}, found)
	if err == nil && reconcileStrategy == ReconcileStrategyInit {
		return nil
	} else if err != nil && !errors.IsNotFound(err) {
		return err
	}

	ownerRef := metav1.OwnerReference{
		UID:        sc.UID,
		APIVersion: sc.APIVersion,
		Kind:       sc.Kind,
		Name:       sc.Name,
	}
	rookConfigData, configErr := getRookCephConfig(r, sc)
	if configErr != nil {
		return fmt.Errorf("failed to get rook ceph config data: %w", err)
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
			return err
		}
		return err
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
		return r.Client.Update(context.TODO(), cm)
	}
	return nil
}

// ensureDeleted is dummy func for the ocsCephConfig
func (obj *ocsCephConfig) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	return nil
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
		rookConfigData, err := updateRookConfig(defaultRookConfigData, globalSectionKey, publicNetworkKey, cidrName)
		if err != nil {
			return "", fmt.Errorf("failed to set network configuration for rook: %v", err)
		}
		return rookConfigData, nil
	}
	return defaultRookConfigData, nil
}
