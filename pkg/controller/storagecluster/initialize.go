package storagecluster

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Initialize is responsible for initializing ocs related resources
func (r *ReconcileStorageCluster) Initialize(request reconcile.Request) error {
	ocsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-install",
			Namespace: request.Namespace,
		},
	}

	// Check for existing ocs install configmap
	log.Info("Checking for existing ocs configmap")
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: ocsConfigMap.GetName(), Namespace: ocsConfigMap.GetNamespace()}, ocsConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("No ocs-install configmap found, triggering the initialization job")
			// TODO: Add logic to install ocs related resources

			// Creating configmap
			err = r.client.Create(context.TODO(), ocsConfigMap)
			if err != nil {
				return fmt.Errorf("Failed to create ocs-install configmap %v", err)
			}
			log.Info(fmt.Sprintf("Successfully created ocs-install configmap: %s", ocsConfigMap.GetName()))
		} else {
			return fmt.Errorf("Failed to get the configmap: %s, err %v", ocsConfigMap.GetName(), err)
		}
	}
	log.Info(fmt.Sprintf("Successfully retrieved the ocs-install configmap: %s", ocsConfigMap.GetName()))
	return nil
}
