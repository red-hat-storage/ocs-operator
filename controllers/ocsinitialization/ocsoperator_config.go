package ocsinitialization

import (
	"context"
	"fmt"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// This configmap is purely for the OCS operator to use
	ocsOperatorConfigName = "ocs-operator-config"
)

// ensureOCSOperatorConfig ensures that the OCS operator config is present in the
// cluster & has the correct values
func (r *OCSInitializationReconciler) ensureOCSOperatorConfig(initialData *ocsv1.OCSInitialization) error {
	ocsOperatorConfig := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ocsOperatorConfigName,
			Namespace: initialData.Namespace,
		},
		Data: map[string]string{
			"CSI_CLUSTER_NAME": r.getClusterID(),
		},
	}
	_, err := ctrl.CreateOrUpdate(context.TODO(), r.Client, ocsOperatorConfig, func() error {
		err := controllerutil.SetControllerReference(initialData, ocsOperatorConfig, r.Scheme)
		if err != nil {
			return fmt.Errorf("failed to set owner reference: %v", err)
		}
		if ocsOperatorConfig.Data["CSI_CLUSTER_NAME"] != r.getClusterID() {
			ocsOperatorConfig.Data["CSI_CLUSTER_NAME"] = r.getClusterID()
			// restart the rook-ceph-operator pod to pick up the new change
			r.restartRookOperatorPod(initialData.Namespace)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return nil
}

// getClusterID returns the cluster ID of the OCP-Cluster
func (r *OCSInitializationReconciler) getClusterID() string {
	clusterVersion := &configv1.ClusterVersion{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "version"}, clusterVersion)
	if err != nil {
		r.Log.Error(err, "Failed to get the clusterVersion version of the OCP cluster")
		return ""
	}
	return fmt.Sprint(clusterVersion.Spec.ClusterID)
}

// restartRookOperatorPod restarts the rook-operator pod in the OCP cluster
func (r *OCSInitializationReconciler) restartRookOperatorPod(namespace string) {
	podList := &corev1.PodList{}
	err := r.Client.List(context.TODO(), podList, client.InNamespace(namespace), client.MatchingLabels{"app": "rook-ceph-operator"})
	if err != nil {
		r.Log.Error(err, "Failed to list rook-ceph-operator pod")
		return
	}
	for _, pod := range podList.Items {
		err := r.Client.Delete(context.TODO(), &pod)
		if err != nil {
			r.Log.Error(err, "Failed to delete rook-ceph-operator pod")
			return
		}
	}
}
