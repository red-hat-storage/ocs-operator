package storagecluster

import (
	"context"
	"fmt"

	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	statusutil "github.com/openshift/ocs-operator/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsNoobaaSystem struct{}

func (obj *ocsNoobaaSystem) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	// Everything other than ReconcileStrategyIgnore means we reconcile
	if sc.Spec.MultiCloudGateway != nil {
		reconcileStrategy := ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyIgnore || reconcileStrategy == ReconcileStrategyStandalone {
			return nil
		}
	}

	// find cephCluster
	foundCeph := &cephv1.CephCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, foundCeph)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Waiting on ceph cluster to be created before starting noobaa")
			return nil
		}
		return err
	}
	if !sc.Spec.ExternalStorage.Enable {
		if foundCeph.Status.State != cephv1.ClusterStateCreated {
			r.Log.Info("Waiting on ceph cluster to initialize before starting noobaa")
			return nil
		}
	} else {
		if foundCeph.Status.State != cephv1.ClusterStateConnected {
			r.Log.Info("Waiting for the external ceph cluster to be connected before starting noobaa")
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
	err = controllerutil.SetControllerReference(sc, nb, r.Scheme)
	if err != nil {
		return err
	}

	// Reconcile the noobaa state, creating or updating if needed
	_, err = controllerutil.CreateOrUpdate(context.TODO(), r.Client, nb, func() error {
		return r.setNooBaaDesiredState(nb, sc)
	})
	if err != nil {
		r.Log.Error(err, "Failed to create or update NooBaa system")
		return err
	}
	// Need to happen after the noobaa CR update was confirmed
	sc.Status.Images.NooBaaCore.ActualImage = *nb.Spec.Image
	sc.Status.Images.NooBaaDB.ActualImage = *nb.Spec.DBImage

	objectRef, err := reference.GetReference(r.Scheme, nb)
	if err != nil {
		return err
	}
	err = objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	if err != nil {
		return err
	}

	statusutil.MapNoobaaNegativeConditions(&r.conditions, nb)
	return nil
}

func (r *StorageClusterReconciler) setNooBaaDesiredState(nb *nbv1.NooBaa, sc *ocsv1.StorageCluster) error {
	storageClassName := generateNameForCephBlockPoolSC(sc)
	coreResources := defaults.GetDaemonResources("noobaa-core", sc.Spec.Resources)
	dbResources := defaults.GetDaemonResources("noobaa-db", sc.Spec.Resources)
	dBVolumeResources := defaults.GetDaemonResources("noobaa-db-vol", sc.Spec.Resources)
	endpointResources := defaults.GetDaemonResources("noobaa-endpoint", sc.Spec.Resources)

	nb.Labels = map[string]string{
		"app": "noobaa",
	}
	nb.Spec.DBStorageClass = &storageClassName
	nb.Spec.PVPoolDefaultStorageClass = &storageClassName
	nb.Spec.CoreResources = &coreResources
	nb.Spec.DBResources = &dbResources
	placement := getPlacement(sc, "noobaa-core")
	nb.Spec.Tolerations = placement.Tolerations
	nb.Spec.Affinity = &corev1.Affinity{NodeAffinity: placement.NodeAffinity}
	nb.Spec.DBVolumeResources = &dBVolumeResources
	nb.Spec.Image = &r.images.NooBaaCore
	nb.Spec.DBImage = &r.images.NooBaaDB
	nb.Spec.DBType = nbv1.DBTypePostgres

	// Default endpoint spec.
	nb.Spec.Endpoints = &nbv1.EndpointsSpec{
		MinCount:               1,
		MaxCount:               2,
		AdditionalVirtualHosts: []string{},

		// TODO: After spec.resources["noobaa-endpoint"] is decleared obesolete this
		// definition should hold a constant value. and should not be read from
		// GetDaemonResources()
		Resources: &endpointResources,
	}

	// Override with MCG options specified in the storage cluster spec
	if sc.Spec.MultiCloudGateway != nil {
		dbStorageClass := sc.Spec.MultiCloudGateway.DbStorageClassName
		if dbStorageClass != "" {
			nb.Spec.DBStorageClass = &dbStorageClass
			nb.Spec.PVPoolDefaultStorageClass = &dbStorageClass
		}
		if sc.Spec.MultiCloudGateway.Endpoints != nil {
			epSpec := sc.Spec.MultiCloudGateway.Endpoints

			nb.Spec.Endpoints.MinCount = epSpec.MinCount
			nb.Spec.Endpoints.MaxCount = epSpec.MaxCount
			if epSpec.AdditionalVirtualHosts != nil {
				nb.Spec.Endpoints.AdditionalVirtualHosts = epSpec.AdditionalVirtualHosts
			}
			if epSpec.Resources != nil {
				nb.Spec.Endpoints.Resources = epSpec.Resources
			}
		}
	}

	// Add KMS details to Noobaa spec, only if
	// cluster-wide encryption is enabled (ie; sc.Spec.Encryption.Enable is True)
	// and KMS ConfigMap is available
	if sc.Spec.Encryption.Enable {
		if kmsConfig, err := getKMSConfigMap(KMSConfigMapName, sc, r.Client); err != nil {
			return err
		} else if kmsConfig != nil {
			nb.Spec.Security.KeyManagementService.ConnectionDetails = kmsConfig.Data
			nb.Spec.Security.KeyManagementService.TokenSecretName = KMSTokenSecretName
		}
	}

	return nil
}

// ensureDeleted Delete noobaa system in the namespace
func (obj *ocsNoobaaSystem) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	// Delete only if this is being managed by the OCS operator
	if sc.Spec.MultiCloudGateway != nil {
		reconcileStrategy := ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy)
		if reconcileStrategy == ReconcileStrategyIgnore || reconcileStrategy == ReconcileStrategyStandalone {
			return nil
		}
	}
	noobaa := &nbv1.NooBaa{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "noobaa", Namespace: sc.Namespace}, noobaa)
	if err != nil {
		if errors.IsNotFound(err) {
			pvcs := &corev1.PersistentVolumeClaimList{}
			opts := []client.ListOption{
				client.InNamespace(sc.Namespace),
				client.MatchingLabels(map[string]string{"noobaa-core": "noobaa"}),
			}
			err = r.Client.List(context.TODO(), pvcs, opts...)
			if err != nil {
				return err
			}
			if len(pvcs.Items) > 0 {
				return fmt.Errorf("Uninstall: Waiting on NooBaa PVCs to be deleted")
			}
			r.Log.Info("Uninstall: NooBaa and noobaa-db PVC not found.")
			return nil
		}
		return fmt.Errorf("Uninstall: Failed to retrieve NooBaa system: %v", err)
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
		r.Log.Info("Uninstall: NooBaa object found, but ownerReference not set to storagecluster. Skipping", "Object", noobaa.ObjectMeta.Name)
		return nil
	}

	if noobaa.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting NooBaa system", "Object", noobaa.ObjectMeta.Name)
		err = r.Client.Delete(context.TODO(), noobaa)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete NooBaa system", "Object", noobaa.ObjectMeta.Name)
			return fmt.Errorf("Uninstall: Failed to delete NooBaa system %v : %v", noobaa.ObjectMeta.Name, err)
		}
	}
	return fmt.Errorf("Uninstall: Waiting on NooBaa system %v to be deleted", noobaa.ObjectMeta.Name)
}
