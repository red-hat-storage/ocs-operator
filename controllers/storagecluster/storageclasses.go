package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/v4/api/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storageClassSkippedError      = "some StorageClasses were skipped while waiting for pre-requisites to be met"
	defaultStorageClassAnnotation = "storageclass.kubernetes.io/is-default-class"
)

// StorageClassConfiguration provides configuration options for a StorageClass.
type StorageClassConfiguration struct {
	storageClass      *storagev1.StorageClass
	reconcileStrategy ReconcileStrategy
	disable           bool
	isClusterExternal bool
}

type ocsStorageClass struct{}

// ensureCreated ensures that StorageClass resources exist in the desired
// state.
func (obj *ocsStorageClass) ensureCreated(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	scs, err := r.newStorageClassConfigurations(instance)
	if err != nil {
		return reconcile.Result{}, err
	}

	err = r.createStorageClasses(scs)
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the storageClasses that the ocs-operator created
func (obj *ocsStorageClass) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	sccs, err := r.newStorageClassConfigurations(instance)
	if err != nil {
		r.Log.Error(err, "Uninstall: Unable to determine the StorageClass names.") //nolint:gosimple
		return reconcile.Result{}, nil
	}
	for _, scc := range sccs {
		sc := scc.storageClass
		existing := storagev1.StorageClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			if existing.DeletionTimestamp != nil {
				r.Log.Info("Uninstall: StorageClass is already marked for deletion.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
				break
			}

			r.Log.Info("Uninstall: Deleting StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
			existing.ObjectMeta.OwnerReferences = sc.ObjectMeta.OwnerReferences
			sc.ObjectMeta = existing.ObjectMeta

			err = r.Client.Delete(context.TODO(), sc)
			if err != nil {
				r.Log.Error(err, "Uninstall: Ignoring error deleting the StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
			}
		case errors.IsNotFound(err):
			r.Log.Info("Uninstall: StorageClass not found, nothing to do.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
		default:
			r.Log.Error(err, "Uninstall: Error while getting StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterReconciler) createStorageClasses(sccs []StorageClassConfiguration) error {
	operatorNamespace, err := util.GetOperatorNamespace()
	if err != nil {
		return err
	}
	var skippedSC []string
	for _, scc := range sccs {
		if scc.reconcileStrategy == ReconcileStrategyIgnore || scc.disable {
			continue
		}
		sc := scc.storageClass

		switch {
		case (strings.Contains(sc.Name, "-ceph-rbd") || strings.Contains(sc.Provisioner, fmt.Sprintf("%s.rbd.csi.ceph.com", operatorNamespace))) && !scc.isClusterExternal:
			// wait for CephBlockPool to be ready
			cephBlockPool := cephv1.CephBlockPool{}
			key := types.NamespacedName{Name: sc.Parameters["pool"], Namespace: sc.Parameters["clusterID"]}
			err := r.Client.Get(context.TODO(), key, &cephBlockPool)
			if err != nil || cephBlockPool.Status == nil || cephBlockPool.Status.Phase != cephv1.ConditionType(util.PhaseReady) {
				r.Log.Info("Waiting for CephBlockPool to be Ready. Skip reconciling StorageClass",
					"CephBlockPool", klog.KRef(key.Namespace, key.Name),
					"StorageClass", klog.KRef("", sc.Name),
				)
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
		case (strings.Contains(sc.Name, "-ceph-non-resilient-rbd") || sc.Parameters["topologyConstrainedPools"] != "") && !scc.isClusterExternal:
			// wait for CephBlockPools to be ready
			cephBlockPools := cephv1.CephBlockPoolList{}
			err := r.Client.List(context.TODO(), &cephBlockPools, client.InNamespace(sc.Parameters["clusterID"]))
			if err != nil {
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
			num := strings.Count(sc.Parameters["topologyConstrainedPools"], "poolName")
			var counter = 0
			// Waiting for all the non-resilient cephblockpools to be ready
			for _, cephBlockPool := range cephBlockPools.Items {
				// Do not count the default cephblockpools
				if cephBlockPool.Spec.DeviceClass == "" || cephBlockPool.Spec.DeviceClass == "replicated" {
					continue
				}
				if cephBlockPool.Status != nil && cephBlockPool.Status.Phase == cephv1.ConditionType(util.PhaseReady) {
					counter++
				} else {
					r.Log.Info("Waiting for Non-resilient CephBlockPools to be Ready. Skip reconciling StorageClass",
						"CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name),
						"StorageClass", klog.KRef("", sc.Name),
					)
				}
			}
			if counter < num {
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
		case (strings.Contains(sc.Name, "-cephfs") || strings.Contains(sc.Provisioner, fmt.Sprintf("%s.cephfs.csi.ceph.com", operatorNamespace))) && !scc.isClusterExternal:
			// wait for CephFilesystem to be ready
			cephFilesystem := cephv1.CephFilesystem{}
			key := types.NamespacedName{Name: sc.Parameters["fsName"], Namespace: sc.Parameters["clusterID"]}
			err := r.Client.Get(context.TODO(), key, &cephFilesystem)
			if err != nil || cephFilesystem.Status == nil || cephFilesystem.Status.Phase != cephv1.ConditionType(util.PhaseReady) {
				r.Log.Info("Waiting for CephFilesystem to be Ready. Skip reconciling StorageClass",
					"CephFilesystem", klog.KRef(key.Namespace, key.Name),
					"StorageClass", klog.KRef("", sc.Name),
				)
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
		case strings.Contains(sc.Name, "-nfs") || strings.Contains(sc.Provisioner, fmt.Sprintf("%s.nfs.csi.ceph.com", operatorNamespace)):
			// wait for CephNFS to be ready
			cephNFS := cephv1.CephNFS{}
			key := types.NamespacedName{Name: sc.Parameters["nfsCluster"], Namespace: sc.Parameters["clusterID"]}
			err := r.Client.Get(context.TODO(), key, &cephNFS)
			if err != nil || cephNFS.Status == nil || cephNFS.Status.Phase != util.PhaseReady {
				r.Log.Info("Waiting for CephNFS to be Ready. Skip reconciling StorageClass",
					"CephNFS", klog.KRef(key.Namespace, key.Name),
					"StorageClass", klog.KRef("", sc.Name),
				)
				skippedSC = append(skippedSC, sc.Name)
				continue
			}
		}

		existing := &storagev1.StorageClass{}
		err := r.Client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, existing)

		if errors.IsNotFound(err) {
			// Since the StorageClass is not found, we will create a new one
			r.Log.Info("Creating StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
			err = r.Client.Create(context.TODO(), sc)
			if err != nil {
				return err
			}
		} else if err != nil {
			return err
		} else {
			if scc.reconcileStrategy == ReconcileStrategyInit {
				continue
			}
			if existing.DeletionTimestamp != nil {
				return fmt.Errorf("failed to restore StorageClass  %s because it is marked for deletion", existing.Name)
			}
			if !reflect.DeepEqual(sc.Parameters, existing.Parameters) {
				// Since we have to update the existing StorageClass
				// So, we will delete the existing storageclass and create a new one
				r.Log.Info("StorageClass needs to be updated, deleting it.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
				err = r.Client.Delete(context.TODO(), existing)
				if err != nil {
					r.Log.Error(err, "Failed to delete StorageClass.", "StorageClass", klog.KRef(sc.Namespace, existing.Name))
					return err
				}
				r.Log.Info("Creating StorageClass.", "StorageClass", klog.KRef(sc.Namespace, sc.Name))
				err = r.Client.Create(context.TODO(), sc)
				if err != nil {
					r.Log.Info("Failed to create StorageClass.", "StorageClass", klog.KRef(sc.Namespace, sc.Name))
					return err
				}
			}
		}
	}
	if len(skippedSC) > 0 {
		return fmt.Errorf("%s: [%s]", storageClassSkippedError, strings.Join(skippedSC, ","))
	}
	return nil
}

// newCephFilesystemStorageClassConfiguration generates configuration options for a Ceph Filesystem StorageClass.
func newCephFilesystemStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	managementSpec := initData.Spec.ManagedResources.CephFilesystems
	return StorageClassConfiguration{
		storageClass: &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephFilesystemSC(initData),
				Annotations: map[string]string{
					"description": "Provides RWO and RWX Filesystem volumes",
				},
			},
			Provisioner:   fmt.Sprintf("%s.cephfs.csi.ceph.com", os.Getenv(util.OperatorNamespaceEnvVar)),
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
		reconcileStrategy: ReconcileStrategy(managementSpec.ReconcileStrategy),
		disable:           managementSpec.DisableStorageClass,
		isClusterExternal: initData.Spec.ExternalStorage.Enable,
	}
}

// newCephBlockPoolStorageClassConfiguration generates configuration options for a Ceph Block Pool StorageClass.
func newCephBlockPoolStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	managementSpec := initData.Spec.ManagedResources.CephBlockPools
	scc := StorageClassConfiguration{
		storageClass: &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephBlockPoolSC(initData),
				Annotations: map[string]string{
					"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				},
			},
			Provisioner:   fmt.Sprintf("%s.rbd.csi.ceph.com", os.Getenv(util.OperatorNamespaceEnvVar)),
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				"clusterID":                 initData.Namespace,
				"pool":                      generateNameForCephBlockPool(initData),
				"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
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
		reconcileStrategy: ReconcileStrategy(managementSpec.ReconcileStrategy),
		disable:           managementSpec.DisableStorageClass,
		isClusterExternal: initData.Spec.ExternalStorage.Enable,
	}
	if initData.Spec.ManagedResources.CephBlockPools.DefaultStorageClass {
		scc.storageClass.Annotations[defaultStorageClassAnnotation] = "true"
	}
	return scc
}

// newCephBlockPoolVirtualizationStorageClassConfiguration generates configuration options for a Ceph Block Pool StorageClass for virtualization environment.
func newCephBlockPoolVirtualizationStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	virtualizationStorageClassConfig := newCephBlockPoolStorageClassConfiguration(initData)
	meta := &virtualizationStorageClassConfig.storageClass.ObjectMeta
	meta.Name = generateNameForCephBlockPoolVirtualizationSC(initData)
	meta.Annotations["description"] = "Provides RWO and RWX Block volumes suitable for Virtual Machine disks"
	meta.Annotations["storageclass.kubevirt.io/is-default-virt-class"] = "true"
	// remove the default storageClass annotation as it's not meant for the virtualization storageClass
	delete(meta.Annotations, defaultStorageClassAnnotation)
	virtualizationStorageClassConfig.storageClass.Parameters["mounter"] = "rbd"
	virtualizationStorageClassConfig.storageClass.Parameters["mapOptions"] = "krbd:rxbounce"
	return virtualizationStorageClassConfig
}

// newNonResilientCephBlockPoolStorageClassConfiguration generates configuration options for a Non-Resilient Ceph Block Pool StorageClass.
func newNonResilientCephBlockPoolStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	volumeBindingWaitForFirstConsumer := storagev1.VolumeBindingWaitForFirstConsumer
	return StorageClassConfiguration{
		storageClass: &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForNonResilientCephBlockPoolSC(initData),
				Annotations: map[string]string{
					"description": "Ceph Non Resilient Pools : Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				},
			},
			Provisioner:       fmt.Sprintf("%s.rbd.csi.ceph.com", os.Getenv(util.OperatorNamespaceEnvVar)),
			ReclaimPolicy:     &persistentVolumeReclaimDelete,
			VolumeBindingMode: &volumeBindingWaitForFirstConsumer,
			// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				"clusterID":                 initData.Namespace,
				"pool":                      generateNameForCephBlockPool(initData),
				"topologyConstrainedPools":  getTopologyConstrainedPools(initData),
				"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
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
		isClusterExternal: initData.Spec.ExternalStorage.Enable,
	}
}

// newCephNFSStorageClassConfiguration generates configuration options for a Ceph NFS StorageClass.
func newCephNFSStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	return StorageClassConfiguration{
		storageClass: &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephNetworkFilesystemSC(initData),
				Annotations: map[string]string{
					"description": "Provides RWO and RWX Filesystem volumes",
				},
			},
			Provisioner:          generateNameForNFSCSIProvisioner(initData),
			ReclaimPolicy:        &persistentVolumeReclaimDelete,
			AllowVolumeExpansion: &allowVolumeExpansion,
			Parameters: map[string]string{
				"clusterID":        initData.Namespace,
				"nfsCluster":       generateNameForCephNFS(initData),
				"fsName":           generateNameForCephFilesystem(initData),
				"server":           generateNameForNFSService(initData),
				"volumeNamePrefix": "nfs-export-",
				"csi.storage.k8s.io/provisioner-secret-name":            "rook-csi-cephfs-provisioner",
				"csi.storage.k8s.io/provisioner-secret-namespace":       initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":             "rook-csi-cephfs-node",
				"csi.storage.k8s.io/node-stage-secret-namespace":        initData.Namespace,
				"csi.storage.k8s.io/controller-expand-secret-name":      "rook-csi-cephfs-provisioner",
				"csi.storage.k8s.io/controller-expand-secret-namespace": initData.Namespace,
			},
		},
	}
}

// newEncryptedCephBlockPoolStorageClassConfiguration generates configuration options for an encrypted Ceph Block Pool StorageClass.
// when user has asked for PV encryption during deployment.
func newEncryptedCephBlockPoolStorageClassConfiguration(initData *ocsv1.StorageCluster, serviceName string) StorageClassConfiguration {
	allowVolumeExpansion := true
	encryptedStorageClassConfig := newCephBlockPoolStorageClassConfiguration(initData)
	encryptedStorageClassConfig.storageClass.ObjectMeta.Name = generateNameForEncryptedCephBlockPoolSC(initData)
	// adding a annotation to support smart cloning across namespace for encrypted volume
	encryptedStorageClassConfig.storageClass.ObjectMeta.Annotations["cdi.kubevirt.io/clone-strategy"] = "copy"
	encryptedStorageClassConfig.storageClass.Parameters["encrypted"] = "true"
	encryptedStorageClassConfig.storageClass.Parameters["encryptionKMSID"] = serviceName
	encryptedStorageClassConfig.storageClass.AllowVolumeExpansion = &allowVolumeExpansion
	return encryptedStorageClassConfig
}

// newCephOBCStorageClassConfiguration generates configuration options for a Ceph Object Store StorageClass.
func newCephOBCStorageClassConfiguration(initData *ocsv1.StorageCluster) StorageClassConfiguration {
	reclaimPolicy := corev1.PersistentVolumeReclaimDelete
	managementSpec := initData.Spec.ManagedResources.CephObjectStores
	return StorageClassConfiguration{
		storageClass: &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephRgwSC(initData),
				Annotations: map[string]string{
					"description": "Provides Object Bucket Claims (OBCs)",
				},
			},
			Provisioner:   fmt.Sprintf("%s.ceph.rook.io/bucket", os.Getenv(util.OperatorNamespaceEnvVar)),
			ReclaimPolicy: &reclaimPolicy,
			Parameters: map[string]string{
				"objectStoreNamespace": initData.Namespace,
				"region":               "us-east-1",
				"objectStoreName":      generateNameForCephObjectStore(initData),
			},
		},
		reconcileStrategy: ReconcileStrategy(managementSpec.ReconcileStrategy),
		disable:           managementSpec.DisableStorageClass,
		isClusterExternal: initData.Spec.ExternalStorage.Enable,
	}
}

// newStorageClassConfigurations returns the StorageClassConfiguration instances that should be created
// on first run.
func (r *StorageClusterReconciler) newStorageClassConfigurations(initData *ocsv1.StorageCluster) ([]StorageClassConfiguration, error) {
	ret := []StorageClassConfiguration{
		newCephFilesystemStorageClassConfiguration(initData),
		newCephBlockPoolStorageClassConfiguration(initData),
	}
	// If kubevirt crd is present, we create a specialized rbd storageclass for virtualization environment
	kvcrd := &extv1.CustomResourceDefinition{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: "virtualmachines.kubevirt.io", Namespace: ""}, kvcrd)
	if err == nil {
		ret = append(ret, newCephBlockPoolVirtualizationStorageClassConfiguration(initData))
	}
	if initData.Spec.ManagedResources.CephNonResilientPools.Enable {
		ret = append(ret, newNonResilientCephBlockPoolStorageClassConfiguration(initData))
	}
	if initData.Spec.NFS != nil && initData.Spec.NFS.Enable {
		ret = append(ret, newCephNFSStorageClassConfiguration(initData))
	}
	// OBC storageclass will be returned only in TWO conditions,
	// a. either 'externalStorage' is enabled
	// OR
	// b. current platform is not a cloud-based platform
	skip, err := r.PlatformsShouldSkipObjectStore()
	if initData.Spec.ExternalStorage.Enable || err == nil && !skip {
		ret = append(ret, newCephOBCStorageClassConfiguration(initData))
	}
	// encrypted Ceph Block Pool storageclass will be returned only if
	// storage-class encryption + kms is enabled and KMS ConfigMap is available
	if initData.Spec.Encryption.StorageClass && initData.Spec.Encryption.KeyManagementService.Enable {
		kmsConfig, err := getKMSConfigMap(KMSConfigMapName, initData, r.Client)
		if err == nil && kmsConfig != nil {
			serviceName := kmsConfig.Data["KMS_SERVICE_NAME"]
			ret = append(ret, newEncryptedCephBlockPoolStorageClassConfiguration(initData, serviceName))
		} else {
			r.Log.Error(err, "Error while getting ConfigMap.", "ConfigMap", klog.KRef(initData.Namespace, KMSConfigMapName))
		}
	}

	return ret, nil
}

func getTopologyConstrainedPools(initData *ocsv1.StorageCluster) string {
	type topologySegment struct {
		DomainLabel string `json:"domainLabel"`
		DomainValue string `json:"value"`
	}
	// TopologyConstrainedPool stores the pool name and a list of its associated topology domain values.
	type topologyConstrainedPool struct {
		PoolName       string            `json:"poolName"`
		DomainSegments []topologySegment `json:"domainSegments"`
	}

	var topologyConstrainedPools []topologyConstrainedPool
	for _, failureDomainValue := range initData.Status.FailureDomainValues {
		failureDomain := initData.Status.FailureDomain
		// Normally the label on the nodes is of the form kubernetes.io/hostname=<hostname>
		// and the same is passed to ceph-csi through rook-ceph-opeartor-config cm.
		// Hence, the ceph-non-resilient-rbd storageclass needs to have domainLabel set as hostname for topology constrained pools.
		if failureDomain == "host" {
			failureDomain = "hostname"
		}
		topologyConstrainedPools = append(topologyConstrainedPools, topologyConstrainedPool{
			PoolName: generateNameForNonResilientCephBlockPool(initData, failureDomainValue),
			DomainSegments: []topologySegment{
				{
					DomainLabel: failureDomain,
					DomainValue: failureDomainValue,
				},
			},
		})
	}
	// returning as string as parameters are of type map[string]string
	topologyConstrainedPoolsStr, err := json.MarshalIndent(topologyConstrainedPools, "", "  ")
	if err != nil {
		return ""
	}
	return string(topologyConstrainedPoolsStr)
}
