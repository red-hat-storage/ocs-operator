package expandstorage

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client client.Client, namespace string) {
	switch r.Method {
	case "POST":
		handlePost(w, r, client, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, client client.Client, namespace string) {
	// When ContentLength is 0 that means request body is empty
	var err error
	if r.ContentLength == 0 {
		klog.Errorf("body in the request is required")
		http.Error(w, "body in the request is required", http.StatusBadRequest)
		return
	}

	type poolDetails struct {
		VolumeType           string `json:"volumeType"`
		PoolName             string `json:"poolName"`
		DataProtectionPolicy int    `json:"dataProtectionPolicy"`
		EnableCompression    bool   `json:"enableCompression"`
		FilesystemName       string `json:"filesystemName"`
		FailureDomain        string `json:"failureDomain"`
	}

	type storageClassDetails struct {
		ReclaimPolicy                string `json:"reclaimPolicy"`
		Name                         string `json:"name"`
		VolumeBindingMode            string `json:"volumeBindingMode"`
		EnableStorageClassEncryption bool   `json:"enableStorageClassEncryption"`
		EncryptionKMSID              string `json:"encryptionKMSID"`
	}

	var ExpandStorage = struct {
		StorageClassForOSDs string              `json:"storageClassForOSDs"`
		EnableEncryption    bool                `json:"enableEncryption"`
		Storage             string              `json:"storage"`
		Replica             int                 `json:"replica"`
		Count               int                 `json:"count"`
		StorageClusterName  string              `json:"storageClusterName"`
		PoolDetails         poolDetails         `json:"poolDetails"`
		StorageClassDetails storageClassDetails `json:"storageClassDetails"`
	}{}

	if err = json.NewDecoder(r.Body).Decode(&ExpandStorage); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	storageCluster := &ocsv1.StorageCluster{}
	err = client.Get(r.Context(), types.NamespacedName{Name: ExpandStorage.StorageClusterName, Namespace: namespace}, storageCluster)
	if err != nil {
		klog.Errorf("failed to get storageCluster: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// update storageCluster
	updateStorageCluster(w, r, client, ExpandStorage.StorageClassForOSDs, ExpandStorage.EnableEncryption, ExpandStorage.Storage, ExpandStorage.Count, ExpandStorage.Replica, storageCluster)

	if ExpandStorage.PoolDetails.VolumeType == "block" {
		klog.Info("Creating cephBlockPool and storageClass")
		// create cephBlockPool
		createCephBlockPool(w, r, client, ExpandStorage.PoolDetails.PoolName, ExpandStorage.StorageClassForOSDs, ExpandStorage.PoolDetails.DataProtectionPolicy, ExpandStorage.PoolDetails.EnableCompression, namespace, ExpandStorage.PoolDetails.FailureDomain, storageCluster.Spec.Arbiter.Enable)

		// create storageClass
		createCephBlockPoolStorageClass(w, r, client, ExpandStorage.StorageClassDetails.Name, ExpandStorage.PoolDetails.PoolName, ExpandStorage.StorageClassDetails.ReclaimPolicy, ExpandStorage.StorageClassDetails.VolumeBindingMode, ExpandStorage.StorageClassDetails.EnableStorageClassEncryption, ExpandStorage.StorageClassDetails.EncryptionKMSID)
	} else if ExpandStorage.PoolDetails.VolumeType == "filesystem" {
		klog.Info("Creating cephFilesystem dataPool and storageClass")
		// create cephFilesystem dataPool
		createCephFilesystemDataPool(w, r, client, ExpandStorage.PoolDetails.PoolName, ExpandStorage.StorageClassForOSDs, ExpandStorage.PoolDetails.DataProtectionPolicy, ExpandStorage.PoolDetails.EnableCompression, ExpandStorage.PoolDetails.FailureDomain, storageCluster)

		// create storageClass
		createCephFilesystemStorageClass(w, r, client, ExpandStorage.StorageClassDetails.Name, ExpandStorage.PoolDetails.PoolName, ExpandStorage.StorageClassDetails.ReclaimPolicy, ExpandStorage.StorageClassDetails.VolumeBindingMode, ExpandStorage.PoolDetails.FilesystemName)
	} else {
		klog.Errorf("invalid volumeType: %s", ExpandStorage.PoolDetails.VolumeType)
		http.Error(w, "invalid volumeType", http.StatusBadRequest)
		return
	}

}

func updateStorageCluster(w http.ResponseWriter, r *http.Request, client client.Client, storageClassForOSDs string, enableEncryption bool, storage string, count, replica int, storageCluster *ocsv1.StorageCluster) {
	klog.Infof("Updating storageCluster %q", storageCluster.Name)
	storageQty := resource.MustParse(storage)
	volumeMode := corev1.PersistentVolumeBlock
	deviceSet := ocsv1.StorageDeviceSet{
		Name:        storageClassForOSDs,
		Count:       count,
		Replica:     replica,
		Portable:    false,
		Encrypted:   &enableEncryption,
		DeviceClass: storageClassForOSDs,
		DeviceType:  "SSD",
		DataPVCTemplate: corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				AccessModes:      []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
				StorageClassName: &storageClassForOSDs,
				VolumeMode:       &volumeMode,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: storageQty,
					},
				},
			},
		},
	}

	storageCluster.Spec.StorageDeviceSets = append(storageCluster.Spec.StorageDeviceSets, deviceSet)
	err := client.Update(r.Context(), storageCluster)
	if err != nil {
		klog.Errorf("failed to update storageCluster: %q", storageCluster.Name)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func createCephBlockPool(w http.ResponseWriter, r *http.Request, client client.Client, poolName, storageClassForOSDs string, dataProtectionPolicy int, enableCompression bool, namespace, failureDomain string, arbiter bool) {
	compression := "none"
	if enableCompression {
		compression = "aggressive"
	}
	replicasPerFailureDomain := 1
	if arbiter {
		replicasPerFailureDomain = 2
	}

	cephBlockPool := &cephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      poolName,
			Namespace: namespace,
		},
		Spec: cephv1.NamedBlockPoolSpec{
			PoolSpec: cephv1.PoolSpec{
				FailureDomain:  failureDomain,
				EnableRBDStats: true,
				DeviceClass:    storageClassForOSDs,
				Replicated: cephv1.ReplicatedSpec{
					Size:                     uint(dataProtectionPolicy),
					RequireSafeReplicaSize:   true,
					ReplicasPerFailureDomain: uint(replicasPerFailureDomain),
				},
				Parameters: map[string]string{
					"compression_mode": compression,
				},
				EnableCrushUpdates: ptr.To(true),
			},
		},
	}

	err := client.Create(r.Context(), cephBlockPool)
	if err != nil {
		klog.Errorf("failed to create cephBlockPool: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func createCephBlockPoolStorageClass(w http.ResponseWriter, r *http.Request, client client.Client, storageClassName, poolName, reclaimPolicy, volumeBindingMode string, enableEncryption bool, encryptionKMSID string) {
	allowVolumeExpansion := true
	storageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClassName,
			Annotations: map[string]string{
				"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
				"reclaimspace.csiaddons.openshift.io/schedule": "@weekly",
			},
		},
		Provisioner:       util.RbdDriverName,
		ReclaimPolicy:     (*corev1.PersistentVolumeReclaimPolicy)(&reclaimPolicy),
		VolumeBindingMode: (*storagev1.VolumeBindingMode)(&volumeBindingMode),
		// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
		AllowVolumeExpansion: &allowVolumeExpansion,
		Parameters: map[string]string{
			"pool":                      poolName,
			"imageFeatures":             "layering,deep-flatten,exclusive-lock,object-map,fast-diff",
			"csi.storage.k8s.io/fstype": "ext4",
			"imageFormat":               "2",
			"encrypted":                 strconv.FormatBool(enableEncryption),
		},
	}
	if enableEncryption {
		storageClass.Parameters["encryptionKMSID"] = encryptionKMSID
		storageClass.Annotations["keyrotation.csiaddons.openshift.io/schedule"] = "@weekly"
	}

	err := client.Create(r.Context(), storageClass)
	if err != nil {
		klog.Errorf("failed to create storageClass: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func createCephFilesystemDataPool(w http.ResponseWriter, r *http.Request, client client.Client, poolName, storageClassForOSDs string, dataProtectionPolicy int, enableCompression bool, failureDomain string, storageCluster *ocsv1.StorageCluster) {
	compression := "none"
	if enableCompression {
		compression = "aggressive"
	}

	datapool := cephv1.NamedPoolSpec{
		Name: poolName,
		PoolSpec: cephv1.PoolSpec{
			FailureDomain: failureDomain,
			DeviceClass:   storageClassForOSDs,
			Replicated: cephv1.ReplicatedSpec{
				Size:                   uint(dataProtectionPolicy),
				RequireSafeReplicaSize: true,
			},
			Parameters: map[string]string{
				"compression_mode": compression,
			},
			EnableCrushUpdates: ptr.To(true),
		},
	}

	storageCluster.Spec.ManagedResources.CephFilesystems.AdditionalDataPools = append(storageCluster.Spec.ManagedResources.CephFilesystems.AdditionalDataPools, datapool)
	err := client.Update(r.Context(), storageCluster)
	if err != nil {
		klog.Errorf("failed to update storageCluster: %q", storageCluster.Name)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func createCephFilesystemStorageClass(w http.ResponseWriter, r *http.Request, client client.Client, storageClassName, poolName, reclaimPolicy, volumeBindingMode string, filesystemName string) {
	allowVolumeExpansion := true
	storageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: storageClassName,
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Filesystem volumes",
			},
		},
		Provisioner:       util.CephFSDriverName,
		ReclaimPolicy:     (*corev1.PersistentVolumeReclaimPolicy)(&reclaimPolicy),
		VolumeBindingMode: (*storagev1.VolumeBindingMode)(&volumeBindingMode),
		// AllowVolumeExpansion is set to true to enable expansion of OCS backed Volumes
		AllowVolumeExpansion: &allowVolumeExpansion,
		Parameters: map[string]string{
			"fsName": filesystemName,
			"pool":   fmt.Sprintf("%s-%s", filesystemName, poolName),
		},
	}

	err := client.Create(r.Context(), storageClass)
	if err != nil {
		klog.Errorf("failed to create storageClass: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only POST method should be used to send data to this endpoint %s", r.URL.Path)
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
