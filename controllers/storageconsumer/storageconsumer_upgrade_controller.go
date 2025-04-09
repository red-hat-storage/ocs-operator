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
	"strconv"
	"strings"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
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

	availableServices, err := util.GetAvailableServices(ctx, r.Client, storageCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed to get available services configured in StorageCluster: %v", err)
	}

	consumerConfigMap := &corev1.ConfigMap{}
	consumerConfigMap.Name = fmt.Sprintf("storageconsumer-%v", util.FnvHash(storageConsumer.Name))
	consumerConfigMap.Namespace = storageConsumer.Namespace

	data := util.GetStorageConsumerDefaultResourceNames(
		storageConsumer.Name,
		string(storageConsumer.UID),
		availableServices,
	)
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(consumerConfigMap), consumerConfigMap); client.IgnoreNotFound(err) != nil {
		return reconcile.Result{}, err
	}

	if consumerConfigMap.UID != "" {
		// For Provider Mode we supported creating one rbdClaim where we generate clientProfile, secret and rns name using
		// consumer UID and Storage Claim name
		// The name of StorageClass is the same as name of storageClaim in provider mode
		rbdClaimName := util.GenerateNameForCephBlockPoolStorageClass(storageCluster)
		rbdClaimMd5Sum := md5.Sum([]byte(rbdClaimName))
		rbdClientProfile := hex.EncodeToString(rbdClaimMd5Sum[:])
		rbdStorageRequestHash := getStorageRequestName(string(storageConsumer.UID), rbdClaimName)
		rbdNodeSecretName := storageClaimCephCsiSecretName("node", rbdStorageRequestHash)
		rbdProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", rbdStorageRequestHash)
		rbdStorageRequestMd5Sum := md5.Sum([]byte(rbdStorageRequestHash))
		rnsName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(rbdStorageRequestMd5Sum[:16]))

		// For Provider Mode we supported creating one cephFs claim where we generate clientProfile, secret and svg name using
		// consumer UID and Storage Claim name
		// The name of StorageClass is the same as name of storageClaim in provider mode
		cephFsClaimName := util.GenerateNameForCephFilesystemStorageClass(storageCluster)
		cephFsClaimMd5Sum := md5.Sum([]byte(cephFsClaimName))
		cephFSClientProfile := hex.EncodeToString(cephFsClaimMd5Sum[:])
		cephFsStorageRequestHash := getStorageRequestName(string(storageConsumer.UID), cephFsClaimName)
		cephFsNodeSecretName := storageClaimCephCsiSecretName("node", cephFsStorageRequestHash)
		cephFsProvisionerSecretName := storageClaimCephCsiSecretName("provisioner", cephFsStorageRequestHash)
		cephFsStorageRequestMd5Sum := md5.Sum([]byte(cephFsStorageRequestHash))
		svgName := fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsStorageRequestMd5Sum[:16]))

		resourceMap := util.WrapStorageConsumerResourceMap(data)
		resourceMap.ReplaceRbdRadosNamespaceName(rnsName)
		resourceMap.ReplaceSubVolumeGroupName(svgName)
		resourceMap.ReplaceSubVolumeGroupRadosNamespaceName("csi")
		resourceMap.ReplaceRbdClientProfileName(rbdClientProfile)
		resourceMap.ReplaceCephFsClientProfileName(cephFSClientProfile)
		resourceMap.ReplaceCsiRbdNodeSecretName(rbdNodeSecretName)
		resourceMap.ReplaceCsiRbdProvisionerSecretName(rbdProvisionerSecretName)
		resourceMap.ReplaceCsiCephFsNodeSecretName(cephFsNodeSecretName)
		resourceMap.ReplaceCsiCephFsProvisionerSecretName(cephFsProvisionerSecretName)
		consumerConfigMap.Data = data

		if err := r.Client.Create(ctx, consumerConfigMap); err != nil {
			return reconcile.Result{}, err
		}
	}

	spec := &storageConsumer.Spec
	spec.ResourceNameMappingConfigMap.Name = consumerConfigMap.Name
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
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
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
