package storagecluster

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	// externalRgwEndpointLabelName is the name of the label that will be used by Noobaa to configure the S3 endpoint
	externalRgwEndpointLabelName = "rgw-endpoint"
)

func (r *ReconcileStorageCluster) ensureNoobaaSystem(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	// find cephCluster
	foundCeph := &cephv1.CephCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, foundCeph)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Waiting on ceph cluster to be created before starting noobaa")
			return nil
		}
		return err
	}
	if !sc.Spec.ExternalStorage.Enable {
		if foundCeph.Status.State != cephv1.ClusterStateCreated {
			reqLogger.Info("Waiting on ceph cluster to initialize before starting noobaa")
			return nil
		}
	} else {
		if foundCeph.Status.State != cephv1.ClusterStateConnected {
			reqLogger.Info("Waiting for the external ceph cluster to be connected before starting noobaa")
			return nil
		}
	}

	// Take ownership over the noobaa object
	nb := &nbv1.NooBaa{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NooBaa",
			APIVersion: "noobaa.io/v1alpha1'",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "noobaa",
			Namespace: sc.Namespace,
		},
	}
	err = controllerutil.SetControllerReference(sc, nb, r.scheme)
	if err != nil {
		return err
	}

	// Reconcile the noobaa state, creating or updating if needed
	_, err = controllerutil.CreateOrUpdate(context.TODO(), r.client, nb, func() error {
		return r.setNooBaaDesiredState(nb, sc)
	})
	if err != nil {
		reqLogger.Error(err, "Failed to create or update NooBaa system")
		return err
	}

	objectRef, err := reference.GetReference(r.scheme, nb)
	if err != nil {
		return err
	}
	objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)

	statusutil.MapNoobaaNegativeConditions(&r.conditions, nb)
	return nil
}

func (r *ReconcileStorageCluster) setNooBaaDesiredState(nb *nbv1.NooBaa, sc *ocsv1.StorageCluster) error {
	storageClassName := generateNameForCephBlockPoolSC(sc)
	coreResources := defaults.GetDaemonResources("noobaa-core", sc.Spec.Resources)
	dbResources := defaults.GetDaemonResources("noobaa-db", sc.Spec.Resources)
	dBVolumeResources := defaults.GetDaemonResources("noobaa-db-vol", sc.Spec.Resources)
	endpointResources := defaults.GetDaemonResources("noobaa-endpoint", sc.Spec.Resources)

	nb.Labels = map[string]string{
		"app": "noobaa",
	}
	// If independent mode, we pass the endpoint value as a label to Noobaa
	// so it can connect to the RGW S3 endpoint
	if sc.Spec.ExternalStorage.Enable {
		nb.Labels[externalRgwEndpointLabelName] = externalRgwEndpoint
	}
	nb.Spec.DBStorageClass = &storageClassName
	nb.Spec.PVPoolDefaultStorageClass = &storageClassName
	nb.Spec.CoreResources = &coreResources
	nb.Spec.DBResources = &dbResources
	placement := getPlacement(sc, "noobaa-core")
	nb.Spec.Tolerations = placement.Tolerations
	nb.Spec.Affinity = &corev1.Affinity{NodeAffinity: placement.NodeAffinity}
	nb.Spec.DBVolumeResources = &dBVolumeResources
	nb.Spec.Image = &r.noobaaCoreImage
	nb.Spec.DBImage = &r.noobaaDBImage

	if nb.Spec.Endpoints == nil {
		nb.Spec.Endpoints = &nbv1.EndpointsSpec{
			MinCount:  1,
			MaxCount:  1,
			Resources: &endpointResources,
		}
	} else {
		nb.Spec.Endpoints.Resources = &endpointResources
	}

	return nil
}

// Delete noobaa system in the namespace
func (r *ReconcileStorageCluster) deleteNoobaaSystems(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	noobaa := &nbv1.NooBaa{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	if err != nil {
		if errors.IsNotFound(err) {
			pvcs := &corev1.PersistentVolumeClaimList{}
			opts := []client.ListOption{
				client.InNamespace(sc.Namespace),
				client.MatchingLabels(map[string]string{"noobaa-core": "noobaa"}),
			}
			err = r.client.List(context.TODO(), pvcs, opts...)
			if err != nil {
				return err
			}
			if len(pvcs.Items) > 0 {
				return fmt.Errorf("Waiting on NooBaa system and PVCs to be deleted")
			}
			reqLogger.Info("NooBaa and noobaa-core PVC not found.")
			return nil
		}
		return fmt.Errorf("Failed to retrieve NooBaa system: %v", err)
	}

	isOwned := false
	for _, ref := range noobaa.GetOwnerReferences() {
		if ref.Name == sc.Name && ref.Kind == sc.Kind {
			isOwned = true
			break
		}
	}
	if !isOwned {
		// if the noobaa found is not owned by our storagecluster, we skip it from deletion.
		reqLogger.Info("NooBaa object found, but ownerReference not set to storagecluster. Skipping")
		return nil
	}

	if noobaa.GetDeletionTimestamp().IsZero() {
		reqLogger.Info("Deleting NooBaa system")
		err = r.client.Delete(context.TODO(), noobaa)
		if err != nil {
			reqLogger.Error(err, "Failed to delete NooBaa system")
			return fmt.Errorf("Failed to delete NooBaa system: %v", err)
		}
	}
	return fmt.Errorf("Waiting on NooBaa system to be deleted")
}
