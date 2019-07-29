package storagecluster

import (
	"context"
	"fmt"

	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	client "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// Initialize is responsible for initializing ocs related resources
func (r *ReconcileStorageCluster) Initialize(request reconcile.Request) error {
	ocsConfigMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-install",
			Namespace: request.Namespace,
		},
		Data: map[string]string{},
	}

	// Check for existing ocs install configmap
	log.Info("Checking for existing ocs configmap")
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: ocsConfigMap.GetName(), Namespace: ocsConfigMap.GetNamespace()}, ocsConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("No ocs-install configmap found, triggering the initialization job")
			// TODO: Add logic to install ocs related resources
			err = r.initializeResources(ocsConfigMap.GetNamespace())
			if err != nil {
				log.Info(fmt.Sprintf("Failed to initialize %v", err))
				return err
			}
			// Creating configmap
			ocsConfigMap.Data["ocs-initialized"] = "true"
			err = r.client.Create(context.TODO(), ocsConfigMap)
			if err != nil {
				return fmt.Errorf("Failed to create ocs-install configmap %v", err)
			}
			log.Info(fmt.Sprintf("Successfully created ocs-install configmap: %s", ocsConfigMap.GetName()))
		} else {
			return fmt.Errorf("Failed to get the configmap: %s, err %v", ocsConfigMap.GetName(), err)
		}
	}
	if value, present := ocsConfigMap.Data["ocs-initialized"]; present || value != "true" {
		err = r.initializeResources(ocsConfigMap.GetNamespace())
		if err != nil {
			return err
		}
	}
	log.Info(fmt.Sprintf("Successfully retrieved the ocs-install configmap: %s", ocsConfigMap.GetName()))
	return nil
}

func (r *ReconcileStorageCluster) initializeResources(namespace string) error {
	// Initializing ceph storageclass
	ocsCephSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ocs-rook-ceph-block",
		},
		Provisioner: "ceph.rook.io/block",
		Parameters: map[string]string{
			"blockPool":        "replicapool",
			"clusterNamespace": "rook-ceph",
			"fstype":           "xfs",
		},
	}
	err := createResourceOrSkip(context.TODO(), r.client, types.NamespacedName{Name: ocsCephSC.GetName(), Namespace: ocsCephSC.GetNamespace()}, ocsCephSC)
	if err != nil {
		return err
	}

	// Initializing ceph block pool
	ocsCephBlockPool := &rookv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "replicapool",
			Namespace: namespace,
		},
		Spec: rookv1.PoolSpec{
			Replicated: rookv1.ReplicatedSpec{
				Size: 3,
			},
		},
	}
	return createResourceOrSkip(context.TODO(), r.client, types.NamespacedName{Name: ocsCephBlockPool.GetName(), Namespace: ocsCephBlockPool.GetNamespace()}, ocsCephBlockPool)
}

// createResourceOrSkip creates a k8s resource if doesn't exists or skips it
func createResourceOrSkip(context context.Context, client client.Client, key client.ObjectKey, obj runtime.Object) error {
	err := client.Get(context, key, obj)
	if err != nil {
		if apierrors.IsNotFound(err) {
			err := client.Create(context, obj)
			if err != nil {
				return err
			}
			log.Info(fmt.Sprintf("Successfully created %s %s", obj.GetObjectKind().GroupVersionKind().Kind, key.Name))
		} else {
			return err
		}
	}
	log.Info(fmt.Sprintf("%s %s already exists, skipping resource creation", obj.GetObjectKind().GroupVersionKind().Kind, key.Name))
	return nil
}
