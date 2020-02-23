package storagecluster

import (
	"context"

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
	if foundCeph.Status.State != cephv1.ClusterStateCreated {
		reqLogger.Info("Waiting on ceph cluster to initialize before starting noobaa")
		return nil
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

	nb.Labels = map[string]string{
		"app": "noobaa",
	}
	nb.Spec.DBStorageClass = &storageClassName
	nb.Spec.PVPoolDefaultStorageClass = &storageClassName
	nb.Spec.CoreResources = &coreResources
	nb.Spec.DBResources = &dbResources
	nb.Spec.Tolerations = defaults.DaemonPlacements["noobaa-core"].Tolerations
	nb.Spec.Affinity = &corev1.Affinity{NodeAffinity: defaults.DaemonPlacements["noobaa-core"].NodeAffinity}
	nb.Spec.DBVolumeResources = &dBVolumeResources
	nb.Spec.Image = &r.noobaaCoreImage
	nb.Spec.DBImage = &r.noobaaDBImage

	return nil
}

// Delete noobaa system in the namespace
func (r *ReconcileStorageCluster) deleteNoobaaSystems(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (bool, error) {
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
				return false, err
			}
			if len(pvcs.Items) > 0 {
				reqLogger.Info("Waiting on NooBaa system and PVCs to be deleted")
				return false, nil
			}
			reqLogger.Info("NooBaa and noobaa-core PVC not found.")
			return true, nil
		}
		reqLogger.Error(err, "Failed to retrieve NooBaa system")
		return false, err
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
		return true, nil
	}

	if noobaa.GetDeletionTimestamp().IsZero() {
		reqLogger.Info("Deleting NooBaa system")
		err = r.client.Delete(context.TODO(), noobaa)
		if err != nil {
			reqLogger.Error(err, "Failed to delete NooBaa system")
			return false, err
		}
	}
	reqLogger.Info("Waiting on NooBaa system to be deleted")
	return false, nil
}
