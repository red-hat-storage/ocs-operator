package storagecluster

import (
	"fmt"
	"reflect"
	"strconv"

	configv1 "github.com/openshift/api/config/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	v1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	backingStorageClassLabel = "ocs.openshift.io.backingstorageclass"
)

type backingStorageClasses struct{}

// ensureCreated ensures that backing storageclasses are created for the StorageCluster
func (obj *backingStorageClasses) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {

	platform, err := r.platform.getPlatform(r.Client)
	if err != nil {
		return reconcile.Result{}, err
	}
	existingBackingStorageClasses := &v1.StorageClassList{}
	err = r.Client.List(
		r.ctx,
		existingBackingStorageClasses,
		client.MatchingLabels(map[string]string{
			backingStorageClassLabel: strconv.FormatBool(true),
		}),
	)
	if err != nil {
		return reconcile.Result{}, err
	}

	existingStorageClassesByName := map[string]*v1.StorageClass{}
	for i := range existingBackingStorageClasses.Items {
		storageClass := &existingBackingStorageClasses.Items[i]
		existingStorageClassesByName[storageClass.Name] = storageClass
	}

	for i := range sc.Spec.BackingStorageClasses {
		bsc := &sc.Spec.BackingStorageClasses[i]
		err := createOrUpdateBackingStorageclass(r, bsc, platform)
		if err != nil {
			r.Log.Error(err, "Failed to create or update StorageClass.", "StorageClass", bsc.Name)
			continue
		}
		existingStorageClassesByName[bsc.Name] = nil
	}

	hasErrors := false
	for _, storageClass := range existingStorageClassesByName {
		if storageClass == nil {
			continue
		}
		if err := r.Client.Delete(r.ctx, storageClass); err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Unable to delete BackingStorageClass.", "Name", storageClass.Name)
			hasErrors = true
		}
	}
	if hasErrors {
		return reconcile.Result{}, fmt.Errorf("Delete failed on one or more backing storage classes")
	}

	return reconcile.Result{}, nil
}

func createOrUpdateBackingStorageclass(r *StorageClusterReconciler, bsc *ocsv1.BackingStorageClass, platform configv1.PlatformType) error {
	if bsc.Name == "" {
		return fmt.Errorf("backingStorageClass name is empty")
	}
	pvReclaimPolicy := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	volumeBindingMode := v1.VolumeBindingWaitForFirstConsumer

	desiredStorageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: bsc.Name,
			Labels: map[string]string{
				backingStorageClassLabel: strconv.FormatBool(true),
			},
		},
		ReclaimPolicy:        &pvReclaimPolicy,
		AllowVolumeExpansion: &allowVolumeExpansion,
		VolumeBindingMode:    &volumeBindingMode,
		Parameters:           bsc.Parameters,
		Provisioner:          bsc.Provisioner,
	}

	if bsc.Provisioner == "" {
		if platform != configv1.AWSPlatformType {
			return fmt.Errorf("auto detection of provisioner is not supported for %s", platform)
		}
		desiredStorageClass.Provisioner = "ebs.csi.aws.com"
	}

	existingStorageClass := &storagev1.StorageClass{}
	existingStorageClass.Name = desiredStorageClass.Name
	if err := r.Client.Get(r.ctx, types.NamespacedName{Name: desiredStorageClass.Name}, existingStorageClass); err != nil && !errors.IsNotFound(err) {
		return err
	}
	if existingStorageClass.UID == "" {
		// Backing storage class not found. Create a new one.
		if err := r.Client.Create(r.ctx, desiredStorageClass); err != nil {
			return err
		}
	} else if !reflect.DeepEqual(desiredStorageClass.Parameters, existingStorageClass.Parameters) {
		// Since we have to update the existing StorageClass
		// So, we will delete the existing storageclass and create a new one
		r.Log.Info("StorageClass needs to be updated, deleting it.", "StorageClass", desiredStorageClass.Name)
		if err := r.Client.Delete(r.ctx, existingStorageClass); err != nil {
			r.Log.Error(err, "Failed to delete StorageClass.", "StorageClass", existingStorageClass.Name)
			return err
		}
		r.Log.Info("Creating StorageClass.", "StorageClass", desiredStorageClass.Name)
		if err := r.Client.Create(r.ctx, desiredStorageClass); err != nil {
			r.Log.Info("Failed to create StorageClass.", "StorageClass", desiredStorageClass.Name)
			return err
		}
	}

	return nil
}

// ensureDeleted deletes the backing storageclasses
func (obj *backingStorageClasses) ensureDeleted(r *StorageClusterReconciler, _ *ocsv1.StorageCluster) (reconcile.Result, error) {
	existingBackingStorageClasses := &v1.StorageClassList{}
	err := r.Client.List(
		r.ctx,
		existingBackingStorageClasses,
		client.MatchingLabels(map[string]string{
			backingStorageClassLabel: strconv.FormatBool(true),
		}),
	)
	if err != nil {
		return reconcile.Result{}, err
	}
	hasErrors := false
	for i := range existingBackingStorageClasses.Items {
		storageClass := &existingBackingStorageClasses.Items[i]
		if err := r.Client.Delete(r.ctx, storageClass); err != nil && !errors.IsNotFound(err) {
			r.Log.Error(err, "Unable to delete BackingStorageClass.", "Name", storageClass.Name)
			hasErrors = true
		}
	}
	if hasErrors {
		return reconcile.Result{}, fmt.Errorf("Delete failed on one or more backing storage classes")
	}
	return reconcile.Result{}, nil
}
