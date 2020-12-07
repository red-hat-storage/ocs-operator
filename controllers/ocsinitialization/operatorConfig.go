package ocsinitialization

import (
	"context"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const rookCephOperatorConfigName = "rook-ceph-operator-config"

// returns a ConfigMap with default settings for rook-ceph operator
func newRookCephOperatorConfig(namespace string) *corev1.ConfigMap {
	var defaultCSIToleration = `
- key: ` + defaults.NodeTolerationKey + `
  operator: Equal
  value: "true"
  effect: NoSchedule`

	config := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      rookCephOperatorConfigName,
			Namespace: namespace,
		},
	}
	data := make(map[string]string)
	data["CSI_PROVISIONER_TOLERATIONS"] = defaultCSIToleration
	data["CSI_PLUGIN_TOLERATIONS"] = defaultCSIToleration
	data["CSI_LOG_LEVEL"] = "5"
	config.Data = data

	return config
}

func (r *OCSInitializationReconciler) ensureRookCephOperatorConfig(initialData *ocsv1.OCSInitialization) error {
	rookCephOperatorConfig := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookCephOperatorConfigName, Namespace: initialData.Namespace}, rookCephOperatorConfig)
	if err != nil {
		if errors.IsNotFound(err) {
			// If it does not exist, create a ConfigMap with default settings
			return r.Client.Create(context.TODO(), newRookCephOperatorConfig(initialData.Namespace))
		}
		return err
	}
	// If it already exists, do not update. It is up to the user to
	// update the ConfigMap as they see fit. Changes will be picked
	// up by rook operator and reconciled. We do not want to reconcile
	// this ConfigMap. If we do, user changes will be reset to defaults.
	return nil
}
