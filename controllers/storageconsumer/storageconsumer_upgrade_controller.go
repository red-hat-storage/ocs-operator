/*
Copyright 2021 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerUpgradeReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageConsumerUpgradeReconciler) SetupWithManager(mgr ctrl.Manager) error {

	operatorVersionPredicate := predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			obj := e.Object.(*ocsv1alpha1.StorageConsumer)
			return obj != nil && strings.HasPrefix(obj.Status.Client.OperatorVersion, "4.18")
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return false
		},
		UpdateFunc: func(e event.UpdateEvent) bool {
			return false
		},
		GenericFunc: func(e event.GenericEvent) bool {
			return false
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named("StorageConsumerUpgrade").
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(operatorVersionPredicate)).
		Complete(r)
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;watch;create;update
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;create;update
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;watch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpoolradosnamespaces,verbs=get;list;watch;create;update

func (r *StorageConsumerUpgradeReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	log := ctrl.LoggerFrom(ctx)

	storageConsumer := &ocsv1alpha1.StorageConsumer{}
	storageConsumer.Name = request.Name
	storageConsumer.Namespace = request.Namespace

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(storageConsumer), storageConsumer); errors.IsNotFound(err) {
		log.Info("No StorageConsumer resource.")
		// Request object not found, could have been deleted after reconcile request.
		// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
		// Return and don't requeue
		return reconcile.Result{}, nil
	} else if err != nil {
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to retrieve StorageConsumer.")
		return reconcile.Result{}, err
	}

	if !storageConsumer.GetDeletionTimestamp().IsZero() {
		return reconcile.Result{}, nil
	}

	if storageConsumer.Spec.ResourceNameMappingConfigMap.Name != "" {
		return reconcile.Result{}, nil
	}

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, r.Client, storageConsumer.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	consumerConfigMapName := fmt.Sprintf("storageconsumer-%v", util.FnvHash(storageConsumer.Name))

	if err = r.reconcileConsumerConfigMap(ctx, storageCluster, storageConsumer, consumerConfigMapName); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.reconcileStorageRequest(ctx, storageCluster, storageConsumer); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.reconcileStorageConsumer(ctx, storageCluster, storageConsumer, consumerConfigMapName); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileConsumerConfigMap(
	ctx context.Context,
	storageCluster *ocsv1.StorageCluster,
	storageConsumer *ocsv1alpha1.StorageConsumer,
	configMapName string,
) error {

	availableServices, err := util.GetAvailableServices(ctx, r.Client, storageCluster)
	if err != nil {
		return fmt.Errorf("failed to get available services configured in StorageCluster: %v", err)
	}

	data := util.GetStorageConsumerDefaultResourceNames(
		storageConsumer.Name,
		string(storageConsumer.UID),
		availableServices,
	)

	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Name = configMapName
	consumerConfigMap.Namespace = storageConsumer.Namespace

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(consumerConfigMap), consumerConfigMap); client.IgnoreNotFound(err) != nil {
		return err
	}

	if consumerConfigMap.UID == "" {
		// For Provider Mode we supported creating one rbdClaim where we generate clientProfile, secret and rns name using
		// consumer UID and Storage Claim name
		// The name of StorageClass is the same as name of storageClaim in provider mode
		rbdClaimName := util.GenerateNameForCephBlockPoolStorageClass(storageCluster)
		rbdClaimMd5Sum := md5.Sum([]byte(rbdClaimName))
		rbdClientProfile := hex.EncodeToString(rbdClaimMd5Sum[:])
		rbdStorageRequestHash := getStorageRequestHash(string(storageConsumer.UID), rbdClaimName)
		rbdNodeSecretName := storageClaimCephCsiSecretName("node", rbdStorageRequestHash)
		rbdProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", rbdStorageRequestHash)
		rbdStorageRequestName := getStorageRequestName(string(storageConsumer.UID), rbdClaimName)
		rbdStorageRequestMd5Sum := md5.Sum([]byte(rbdStorageRequestName))
		rnsName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(rbdStorageRequestMd5Sum[:16]))

		// For Provider Mode we supported creating one cephFs claim where we generate clientProfile, secret and svg name using
		// consumer UID and Storage Claim name
		// The name of StorageClass is the same as name of storageClaim in provider mode
		cephFsClaimName := util.GenerateNameForCephFilesystemStorageClass(storageCluster)
		cephFsClaimMd5Sum := md5.Sum([]byte(cephFsClaimName))
		cephFSClientProfile := hex.EncodeToString(cephFsClaimMd5Sum[:])
		cephFsStorageRequestHash := getStorageRequestHash(string(storageConsumer.UID), cephFsClaimName)
		cephFsNodeSecretName := storageClaimCephCsiSecretName("node", cephFsStorageRequestHash)
		cephFsProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", cephFsStorageRequestHash)
		cephFsStorageRequestName := getStorageRequestName(string(storageConsumer.UID), cephFsClaimName)
		cephFsStorageRequestMd5Sum := md5.Sum([]byte(cephFsStorageRequestName))
		svgName := fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsStorageRequestMd5Sum[:16]))

		resourceMap := util.WrapStorageConsumerResourceMap(data)
		resourceMap.ReplaceRbdRadosNamespaceName(rnsName)
		resourceMap.ReplaceSubVolumeGroupName(svgName)
		resourceMap.ReplaceSubVolumeGroupRadosNamespaceName("csi")
		resourceMap.ReplaceRbdClientProfileName(rbdClientProfile)
		resourceMap.ReplaceCephFsClientProfileName(cephFSClientProfile)
		resourceMap.ReplaceCsiRbdNodeCephUserName(rbdNodeSecretName)
		resourceMap.ReplaceCsiRbdProvisionerCephUserName(rbdProvisionerSecretName)
		resourceMap.ReplaceCsiCephFsNodeCephUserName(cephFsNodeSecretName)
		resourceMap.ReplaceCsiCephFsProvisionerCephUserName(cephFsProvisionerSecretName)
		consumerConfigMap.Data = data

		if err := r.Client.Create(ctx, consumerConfigMap); err != nil {
			return err
		}
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileStorageRequest(
	ctx context.Context,
	storageCluster *ocsv1.StorageCluster,
	storageConsumer *ocsv1alpha1.StorageConsumer,
) error {

	rbdClaimName := util.GenerateNameForCephBlockPoolStorageClass(storageCluster)
	rbdStorageRequestName := getStorageRequestName(string(storageConsumer.UID), rbdClaimName)
	rbdStorageRequestMd5Sum := md5.Sum([]byte(rbdStorageRequestName))
	rnsName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(rbdStorageRequestMd5Sum[:16]))

	// Removing the onwerRef from rns to preserve it after deletion the StorageRequest
	if err := r.removeStorageRequestOwner(ctx, "CephBlockPoolRadosNamespace", rnsName, storageConsumer.Namespace); err != nil {
		return err
	}

	rbdStorageRequest := &metav1.PartialObjectMetadata{}
	rbdStorageRequest.SetGroupVersionKind(ocsv1alpha1.GroupVersion.WithKind("StorageRequest"))
	rbdStorageRequest.Name = rbdStorageRequestName
	rbdStorageRequest.Namespace = storageConsumer.Namespace
	if err := r.Client.Delete(ctx, rbdStorageRequest); client.IgnoreNotFound(err) != nil {
		return err
	}

	cephFsClaimName := util.GenerateNameForCephFilesystemStorageClass(storageCluster)
	cephFsStorageRequestName := getStorageRequestName(string(storageConsumer.UID), cephFsClaimName)
	cephFsStorageRequestMd5Sum := md5.Sum([]byte(cephFsStorageRequestName))
	svgName := fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsStorageRequestMd5Sum[:16]))

	// Removing the onwerRef from svg to preserve it after deletion the StorageRequest
	if err := r.removeStorageRequestOwner(ctx, "CephFilesystemSubVolumeGroup", svgName, storageConsumer.Namespace); err != nil {
		return err
	}

	cephFsStorageRequest := &metav1.PartialObjectMetadata{}
	cephFsStorageRequest.SetGroupVersionKind(ocsv1alpha1.GroupVersion.WithKind("StorageRequest"))
	cephFsStorageRequest.Name = cephFsStorageRequestName
	cephFsStorageRequest.Namespace = storageConsumer.Namespace
	if err := r.Client.Delete(ctx, rbdStorageRequest); client.IgnoreNotFound(err) != nil {
		return err
	}

	return nil
}

func (r *StorageConsumerUpgradeReconciler) removeStorageRequestOwner(ctx context.Context, kind, name, namespace string) error {
	obj := &metav1.PartialObjectMetadata{}
	obj.SetGroupVersionKind(rookCephv1.SchemeGroupVersion.WithKind(kind))
	obj.Name = name
	obj.Namespace = namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(obj), obj); client.IgnoreNotFound(err) != nil {
		return err
	}
	if obj.UID != "" {
		refs := obj.GetOwnerReferences()
		if idx := slices.IndexFunc(refs, func(owner metav1.OwnerReference) bool {
			return owner.Kind == "StorageRequest"
		}); idx != -1 {
			obj.SetOwnerReferences(slices.Delete(refs, idx, idx+1))
			if err := r.Client.Patch(ctx, obj, client.MergeFrom(obj)); client.IgnoreNotFound(err) != nil {
				return err
			}
		}
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileStorageConsumer(ctx context.Context, storageCluster *ocsv1.StorageCluster, storageConsumer *ocsv1alpha1.StorageConsumer, consumerConfigMapName string) error {
	spec := &storageConsumer.Spec
	spec.ResourceNameMappingConfigMap.Name = consumerConfigMapName
	spec.StorageClasses = []ocsv1alpha1.StorageClassSpec{
		{Name: util.GenerateNameForCephBlockPoolStorageClass(storageCluster)},
		{Name: util.GenerateNameForCephFilesystemStorageClass(storageCluster)},
	}
	spec.VolumeSnapshotClasses = []ocsv1alpha1.VolumeSnapshotClassSpec{
		{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.RbdSnapshotter)},
		{Name: util.GenerateNameForSnapshotClass(storageCluster.Name, util.CephfsSnapshotter)},
	}
	spec.VolumeGroupSnapshotClasses = []ocsv1alpha1.VolumeGroupSnapshotClassSpec{
		{Name: util.GenerateNameForGroupSnapshotClass(storageCluster, util.RbdGroupSnapshotter)},
		{Name: util.GenerateNameForGroupSnapshotClass(storageCluster, util.CephfsGroupSnapshotter)},
	}
	util.AddAnnotation(storageConsumer, util.Is419AdjustedAnnotationKey, strconv.FormatBool(true))

	if err := r.Client.Update(ctx, storageConsumer); client.IgnoreNotFound(err) != nil {
		return err
	}
	return nil
}

// getStorageRequestHash generates a hash for a StorageRequest based
// on the MD5 hash of the StorageClaim name and storageConsumer UUID.
func getStorageRequestHash(consumerUUID, storageClaimName string) string {
	s := struct {
		StorageConsumerUUID string `json:"storageConsumerUUID"`
		StorageClaimName    string `json:"storageClaimName"`
	}{
		consumerUUID,
		storageClaimName,
	}

	requestName, err := json.Marshal(s)
	if err != nil {
		panic("failed to marshal storage class request name")
	}
	md5Sum := md5.Sum(requestName)
	return hex.EncodeToString(md5Sum[:16])
}

// getStorageRequestName generates a name for a StorageRequest resource.
func getStorageRequestName(consumerUUID, storageClaimName string) string {
	return fmt.Sprintf("storagerequest-%s", getStorageRequestHash(consumerUUID, storageClaimName))
}

func storageClaimCephCsiSecretName(secretType, suffix string) string {
	return fmt.Sprintf("ceph-client-%s-%s", secretType, suffix)
}
