package storagecluster

import (
	"context"
	"fmt"
	"reflect"

	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

const (
	// The following constants are the indices at which StorageClasses are returned from newStorageClasses and in
	// which they should be passed to createStorageClasses.
	cephFileSystemIndex  = 0
	cephBlockPoolIndex   = 1
	cephObjectStoreIndex = 2
)

// ensureStorageClasses ensures that StorageClass resources exist in the desired
// state.
func (r *StorageClusterReconciler) ensureStorageClasses(instance *ocsv1.StorageCluster) error {
	scs, err := r.newStorageClasses(instance)
	if err != nil {
		return err
	}

	err = r.createStorageClasses(scs, instance)
	if err != nil {
		return err
	}

	return nil
}

func (r *StorageClusterReconciler) createStorageClasses(scs []*storagev1.StorageClass, instance *ocsv1.StorageCluster) error {
	for index, sc := range scs {
		// In the case of an external cluster, some scs may be unavailable. In this case we should move on.
		if sc == nil {
			continue
		}
		reconcileStrategy := ReconcileStrategyIgnore
		disableStorageClass := false
		switch index {
		case cephFileSystemIndex:
			reconcileStrategy = ReconcileStrategy(instance.Spec.ManagedResources.CephFilesystems.ReconcileStrategy)
			disableStorageClass = instance.Spec.ManagedResources.CephFilesystems.DisableStorageClass
		case cephBlockPoolIndex:
			reconcileStrategy = ReconcileStrategy(instance.Spec.ManagedResources.CephBlockPools.ReconcileStrategy)
			disableStorageClass = instance.Spec.ManagedResources.CephBlockPools.DisableStorageClass
		case cephObjectStoreIndex:
			reconcileStrategy = ReconcileStrategy(instance.Spec.ManagedResources.CephObjectStores.ReconcileStrategy)
			disableStorageClass = instance.Spec.ManagedResources.CephObjectStores.DisableStorageClass
		}
		if reconcileStrategy == ReconcileStrategyIgnore || disableStorageClass {
			continue
		}
		existing := &storagev1.StorageClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, existing)

		if errors.IsNotFound(err) {
			// Since the StorageClass is not found, we will create a new one
			r.Log.Info(fmt.Sprintf("Creating StorageClass %s", sc.Name))
			err = r.Client.Create(context.TODO(), sc)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if reconcileStrategy == ReconcileStrategyInit {
				continue
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("failed to restore storageclass  %s because it is marked for deletion", existing.Name)
			}
			if !reflect.DeepEqual(sc.Parameters, existing.Parameters) {
				// Since we have to update the existing StorageClass
				// So, we will delete the existing storageclass and create a new one
				r.Log.Info(fmt.Sprintf("StorageClass %s needs to be updated, deleting it", existing.Name))
				err = r.Client.Delete(context.TODO(), existing)
				if err != nil {
					return err
				}
				r.Log.Info(fmt.Sprintf("Creating StorageClass %s", sc.Name))
				err = r.Client.Create(context.TODO(), sc)
				if err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func (r *StorageClusterReconciler) newOBCStorageClass(initData *ocsv1.StorageCluster) *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	retSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateNameForCephRgwSC(initData),
			Annotations: map[string]string{
				"description": "Provides Object Bucket Claims (OBCs)",
			},
		},
		Provisioner:   fmt.Sprintf("%s.ceph.rook.io/bucket", initData.Namespace),
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"objectStoreNamespace": initData.Namespace,
			"region":               "us-east-1",
			"objectStoreName":      generateNameForCephObjectStore(initData),
		},
	}
	return retSC
}

// newStorageClasses returns the StorageClass instances that should be created
// on first run.
func (r *StorageClusterReconciler) newStorageClasses(initData *ocsv1.StorageCluster) ([]*storagev1.StorageClass, error) {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	ret := []*storagev1.StorageClass{
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephFilesystemSC(initData),
				Annotations: map[string]string{
					"description": "Provides RWO and RWX Filesystem volumes",
				},
			},
			Provisioner:   fmt.Sprintf("%s.cephfs.csi.ceph.com", initData.Namespace),
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				"clusterID": initData.Namespace,
				"fsName":    fmt.Sprintf("%s-cephfilesystem", initData.Name),
				"csi.storage.k8s.io/provisioner-secret-name":            "rook-csi-cephfs-provisioner",
				"csi.storage.k8s.io/provisioner-secret-namespace":       initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":             "rook-csi-cephfs-node",
				"csi.storage.k8s.io/node-stage-secret-namespace":        initData.Namespace,
				"csi.storage.k8s.io/controller-expand-secret-name":      "rook-csi-cephfs-provisioner",
				"csi.storage.k8s.io/controller-expand-secret-namespace": initData.Namespace,
			},
		},
		{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephBlockPoolSC(initData),
				Annotations: map[string]string{
					"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				},
			},
			Provisioner:   fmt.Sprintf("%s.rbd.csi.ceph.com", initData.Namespace),
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				"clusterID":                 initData.Namespace,
				"pool":                      generateNameForCephBlockPool(initData),
				"imageFeatures":             "layering",
				"csi.storage.k8s.io/fstype": "ext4",
				"imageFormat":               "2",
				"csi.storage.k8s.io/provisioner-secret-name":            "rook-csi-rbd-provisioner",
				"csi.storage.k8s.io/provisioner-secret-namespace":       initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":             "rook-csi-rbd-node",
				"csi.storage.k8s.io/node-stage-secret-namespace":        initData.Namespace,
				"csi.storage.k8s.io/controller-expand-secret-name":      "rook-csi-rbd-provisioner",
				"csi.storage.k8s.io/controller-expand-secret-namespace": initData.Namespace,
			},
		},
	}
	// OBC storageclass will be returned only in TWO conditions,
	// a. either 'externalStorage' is enabled
	// OR
	// b. current platform is not a cloud-based platform
	platform, err := r.Platform.GetPlatform(r.Client)
	if initData.Spec.ExternalStorage.Enable || err == nil && !avoidObjectStore(platform) {
		ret = append(ret, r.newOBCStorageClass(initData))
	}
	return ret, nil
}
