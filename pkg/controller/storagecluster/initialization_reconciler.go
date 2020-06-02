package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	externalClusterDetailsSecret = "rook-ceph-external-cluster-details"
	externalClusterDetailsKey    = "external_cluster_details"
	cephFsStorageClassName       = "cephfs"
	cephRbdStorageClassName      = "ceph-rbd"
	cephRgwStorageClassName      = "ceph-rgw"
)

// ExternalResource containes a list of External Cluster Resources
type ExternalResource struct {
	Kind string            `json:"kind"`
	Data map[string]string `json:"data"`
	Name string            `json:"name"`
}

// ensureStorageClasses ensures that StorageClass resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureStorageClasses(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	if instance.Status.StorageClassesCreated {
		return nil
	}

	scs, err := r.newStorageClasses(instance)
	if err != nil {
		return err
	}
	err = r.createStorageClasses(scs, reqLogger)
	if err != nil {
		return err
	}
	instance.Status.StorageClassesCreated = true
	return nil
}

func (r *ReconcileStorageCluster) createStorageClasses(scs []*storagev1.StorageClass, reqLogger logr.Logger) error {
	for _, sc := range scs {
		existing := storagev1.StorageClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original StorageClass %s", sc.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.client.Update(context.TODO(), sc)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating StorageClass %s", sc.Name))
			err = r.client.Create(context.TODO(), sc)
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// ensureExternalStorageClusterResources ensures that requested resources for the external cluster
// being created
func (r *ReconcileStorageCluster) ensureExternalStorageClusterResources(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	// check for the status boolean value accepted or not
	if instance.Status.ExternalSecretFound {
		return nil
	}
	found := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      externalClusterDetailsSecret,
			Namespace: instance.Namespace,
		},
	}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: found.Name, Namespace: found.Namespace}, found)
	if err != nil {
		return err
	}
	ownerRef := metav1.OwnerReference{
		UID:        instance.UID,
		APIVersion: instance.APIVersion,
		Kind:       instance.Kind,
		Name:       instance.Name,
	}
	var data []ExternalResource
	err = json.Unmarshal(found.Data[externalClusterDetailsKey], &data)
	if err != nil {
		reqLogger.Error(err, "could not parse json blob")
		return err
	}
	scs, err := r.newStorageClasses(instance)
	if err != nil {
		reqLogger.Error(err, "failed to create StorageClasses")
		return err
	}
	for _, d := range data {
		objectMeta := metav1.ObjectMeta{
			Name:            d.Name,
			Namespace:       instance.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		}
		objectKey := types.NamespacedName{Name: d.Name, Namespace: instance.Namespace}
		switch d.Kind {
		case "ConfigMap":
			cm := &corev1.ConfigMap{
				ObjectMeta: objectMeta,
				Data:       d.Data,
			}
			found := &corev1.ConfigMap{ObjectMeta: objectMeta}
			err := r.client.Get(context.TODO(), objectKey, found)
			if err != nil {
				if errors.IsNotFound(err) {
					reqLogger.Info(fmt.Sprintf("creating configmap: %s", cm.Name))
					err = r.client.Create(context.TODO(), cm)
					if err != nil {
						reqLogger.Error(err, "creation of configmap failed")
						return err
					}
				} else {
					reqLogger.Error(err, "unable the get the configmap")
					return err
				}
			}
		case "Secret":
			sec := &corev1.Secret{
				ObjectMeta: objectMeta,
				Data:       make(map[string][]byte),
			}
			for k, v := range d.Data {
				sec.Data[k] = []byte(v)
			}
			found := &corev1.Secret{ObjectMeta: objectMeta}
			err := r.client.Get(context.TODO(), objectKey, found)
			if err != nil {
				if errors.IsNotFound(err) {
					reqLogger.Info(fmt.Sprintf("creating secret: %s", sec.Name))
					err = r.client.Create(context.TODO(), sec)
					if err != nil {
						reqLogger.Error(err, "creation of secret failed")
						return err
					}
				} else {
					reqLogger.Error(err, "unable to get the secret")
					return err
				}
			}
		case "StorageClass":
			var sc *storagev1.StorageClass
			if d.Name == cephFsStorageClassName {
				// Setting the fsname for cephfs StorageClass
				sc = scs[0]
			} else if d.Name == cephRbdStorageClassName {
				// Setting the PoolName for RBD StorageClass
				sc = scs[1]
			} else if d.Name == cephRgwStorageClassName {
				// Setting the Endpoint for OBC StorageClass
				sc = scs[2]
			}
			for k, v := range d.Data {
				sc.Parameters[k] = v
			}
		}
	}
	// creating all the storageClasses once we set the values
	err = r.createStorageClasses(scs, reqLogger)
	if err != nil {
		return err
	}
	instance.Status.ExternalSecretFound = true
	return nil
}

func (r *ReconcileStorageCluster) newOBCStorageClass(initData *ocsv1.StorageCluster) *storagev1.StorageClass {
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	retSC := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: generateNameForCephRgwSC(initData),
		},
		Provisioner:   fmt.Sprintf("%s.ceph.rook.io/bucket", initData.Namespace),
		ReclaimPolicy: &reclaimPolicy,
		Parameters: map[string]string{
			"objectStoreNamespace": initData.Namespace,
			"region":               "us-east-1",
		},
	}

	// On external cluster, Rook-Ceph does not need the objectStoreName since no ObjectStore is deployed
	// This is only valid on converged mode
	if !initData.Spec.ExternalStorage.Enable {
		retSC.Parameters["objectStoreName"] = generateNameForCephObjectStore(initData)
	}
	return retSC
}

// newStorageClasses returns the StorageClass instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newStorageClasses(initData *ocsv1.StorageCluster) ([]*storagev1.StorageClass, error) {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	ret := []*storagev1.StorageClass{
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephFilesystemSC(initData),
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
		&storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephBlockPoolSC(initData),
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
	platform, err := r.platform.GetPlatform(r.client)
	if initData.Spec.ExternalStorage.Enable || err == nil && !isValidCloudPlatform(platform) {
		ret = append(ret, r.newOBCStorageClass(initData))
	}
	return ret, nil
}

// ensureCephObjectStores ensures that CephObjectStore resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureCephObjectStores(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if instance.Status.CephObjectStoresCreated {
		return nil
	}
	platform, err := r.platform.GetPlatform(r.client)
	if err != nil {
		return err
	}
	if isValidCloudPlatform(platform) {
		reqLogger.Info(fmt.Sprintf("not creating a CephObjectStore because the platform is '%s'", platform))
		return nil
	}

	cephObjectStores, err := r.newCephObjectStoreInstances(instance)
	if err != nil {
		return err
	}
	for _, cephObjectStore := range cephObjectStores {
		existing := cephv1.CephObjectStore{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStore %s", cephObjectStore.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStore.ObjectMeta.OwnerReferences
			cephObjectStore.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), cephObjectStore)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating CephObjectStore %s", cephObjectStore.Name))
			err = r.client.Create(context.TODO(), cephObjectStore)
			if err != nil {
				return err
			}
		}
	}

	instance.Status.CephObjectStoresCreated = true

	return nil
}

// newCephObjectStoreInstances returns the cephObjectStore instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newCephObjectStoreInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephObjectStore, error) {
	ret := []*cephv1.CephObjectStore{
		&cephv1.CephObjectStore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				PreservePoolsOnDelete: false,
				DataPool: cephv1.PoolSpec{
					FailureDomain: initData.Status.FailureDomain,
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				MetadataPool: cephv1.PoolSpec{
					FailureDomain: initData.Status.FailureDomain,
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				Gateway: cephv1.GatewaySpec{
					Port:      80,
					Instances: 2,
					Placement: defaults.DaemonPlacements["rgw"],
					Resources: defaults.GetDaemonResources("rgw", initData.Spec.Resources),
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCephBlockPools ensures that cephBlockPool resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureCephBlockPools(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	if instance.Status.CephBlockPoolsCreated {
		return nil
	}

	cephBlockPools, err := r.newCephBlockPoolInstances(instance)
	if err != nil {
		return err
	}
	for _, cephBlockPool := range cephBlockPools {
		existing := cephv1.CephBlockPool{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: cephBlockPool.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original cephBlockPool %s", cephBlockPool.Name))
			existing.ObjectMeta.OwnerReferences = cephBlockPool.ObjectMeta.OwnerReferences
			cephBlockPool.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), cephBlockPool)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephBlockPool %s", cephBlockPool.Name))
			err = r.client.Create(context.TODO(), cephBlockPool)
			if err != nil {
				return err
			}
		}
	}

	instance.Status.CephBlockPoolsCreated = true

	return nil
}

// newCephBlockPoolInstances returns the cephBlockPool instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newCephBlockPoolInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephBlockPool, error) {
	ret := []*cephv1.CephBlockPool{
		&cephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephBlockPool(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.PoolSpec{
				FailureDomain: initData.Status.FailureDomain,
				Replicated: cephv1.ReplicatedSpec{
					Size:            3,
					TargetSizeRatio: .49,
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCephObjectStoreUsers ensures that cephObjectStoreUser resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureCephObjectStoreUsers(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if instance.Status.CephObjectStoreUsersCreated {
		return nil
	}
	platform, err := r.platform.GetPlatform(r.client)
	if err != nil {
		return err
	}
	if isValidCloudPlatform(platform) {
		return nil
	}

	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(instance)
	if err != nil {
		return err
	}
	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		existing := cephv1.CephObjectStoreUser{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: cephObjectStoreUser.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStoreUser %s", cephObjectStoreUser.Name))
			existing.ObjectMeta.OwnerReferences = cephObjectStoreUser.ObjectMeta.OwnerReferences
			cephObjectStoreUser.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephObjectStoreUser %s", cephObjectStoreUser.Name))
			err = r.client.Create(context.TODO(), cephObjectStoreUser)
			if err != nil {
				return err
			}
		}
	}

	instance.Status.CephObjectStoreUsersCreated = true

	return err
}

// newCephObjectStoreUserInstances returns the cephObjectStoreUser instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newCephObjectStoreUserInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephObjectStoreUser, error) {
	ret := []*cephv1.CephObjectStoreUser{
		&cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStoreUser(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreUserSpec{
				DisplayName: initData.Name,
				Store:       generateNameForCephObjectStore(initData),
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}

// ensureCephFilesystems ensures that cephFilesystem resources exist in the desired
// state.
func (r *ReconcileStorageCluster) ensureCephFilesystems(instance *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	if instance.Status.CephFilesystemsCreated {
		return nil
	}

	cephFilesystems, err := r.newCephFilesystemInstances(instance)
	if err != nil {
		return err
	}
	for _, cephFilesystem := range cephFilesystems {
		existing := cephv1.CephFilesystem{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: cephFilesystem.Namespace}, &existing)
		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				reqLogger.Info(fmt.Sprintf("Unable to restore init object because %s is marked for deletion", existing.Name))
				return fmt.Errorf("failed to restore initialization object %s because it is marked for deletion", existing.Name)
			}

			reqLogger.Info(fmt.Sprintf("Restoring original cephFilesystem %s", cephFilesystem.Name))
			existing.ObjectMeta.OwnerReferences = cephFilesystem.ObjectMeta.OwnerReferences
			cephFilesystem.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), cephFilesystem)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephFilesystem %s", cephFilesystem.Name))
			err = r.client.Create(context.TODO(), cephFilesystem)
			if err != nil {
				return err
			}
		}
	}

	instance.Status.CephFilesystemsCreated = true

	return nil
}

// newCephFilesystemInstances returns the cephFilesystem instances that should be created
// on first run.
func (r *ReconcileStorageCluster) newCephFilesystemInstances(initData *ocsv1.StorageCluster) ([]*cephv1.CephFilesystem, error) {
	ret := []*cephv1.CephFilesystem{
		&cephv1.CephFilesystem{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephFilesystem(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.FilesystemSpec{
				MetadataPool: cephv1.PoolSpec{
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
					FailureDomain: initData.Status.FailureDomain,
				},
				DataPools: []cephv1.PoolSpec{
					cephv1.PoolSpec{
						Replicated: cephv1.ReplicatedSpec{
							Size:            3,
							TargetSizeRatio: .49,
						},
						FailureDomain: initData.Status.FailureDomain,
					},
				},
				MetadataServer: cephv1.MetadataServerSpec{
					ActiveCount:   1,
					ActiveStandby: true,
					Placement:     getPlacement(initData, "mds"),
					Resources:     defaults.GetDaemonResources("mds", initData.Spec.Resources),
				},
			},
		},
	}
	for _, obj := range ret {
		err := controllerutil.SetControllerReference(initData, obj, r.scheme)
		if err != nil {
			return nil, err
		}
	}
	return ret, nil
}
