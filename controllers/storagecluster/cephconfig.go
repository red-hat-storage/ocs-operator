package storagecluster

import (
	"context"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
)

type ocsCephConfig struct{}

const (
	rookConfigMapName     = "rook-config-override"
	defaultRookConfigData = `
[global]
bdev_flock_retry = 20
mon_osd_full_ratio = .85
mon_osd_backfillfull_ratio = .8
mon_osd_nearfull_ratio = .75
mon_max_pg_per_osd = 600
mon_pg_warn_max_object_skew = 0
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
	}

	ownerRef := metav1.OwnerReference{
		UID:        sc.UID,
		APIVersion: sc.APIVersion,
		Kind:       sc.Kind,
		Name:       sc.Name,
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rookConfigMapName,
			Namespace:       sc.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Data: map[string]string{
			"config": defaultRookConfigData,
		},
	}

	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating Ceph ConfigMap.", "ConfigMap", klog.KRef(sc.Namespace, rookConfigMapName))
			err = r.Client.Create(context.TODO(), cm)
			if err != nil {
				r.Log.Error(err, "Failed to create Ceph ConfigMap.", "ConfigMap", klog.KRef(sc.Namespace, rookConfigMapName))
				return err
			}
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
