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

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
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
			_, ok := e.Object.GetAnnotations()[util.TicketAnnotation]
			return ok
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
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests/finalizers,verbs=update
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpoolradosnamespaces,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclients,verbs=get
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclients/status,verbs=patch

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

	if storageConsumer.Status.Client == nil {
		return reconcile.Result{}, fmt.Errorf("StorageConsumer Status is empty")
	}

	storageCluster, err := util.GetStorageClusterInNamespace(ctx, r.Client, storageConsumer.Namespace)
	if err != nil {
		return reconcile.Result{}, err
	}

	clusterID := util.GetClusterID(ctx, r.Client, &log)
	if clusterID == "" {
		return reconcile.Result{}, fmt.Errorf("failed to get openshift cluster ID")
	}

	isInternalConsumer := clusterID == storageConsumer.Status.Client.ClusterID && storageConsumer.Status.Client.Name == storageCluster.Name

	if !isInternalConsumer {
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
	} else {
		if err = r.reconcileConsumerConfigMap(ctx, storageCluster, storageConsumer, defaults.LocalStorageConsumerConfigMapName); err != nil {
			return reconcile.Result{}, err
		}

		if err = r.reconcileStorageRequest(ctx, storageCluster, storageConsumer); err != nil {
			return reconcile.Result{}, err
		}

		newStorageConsumer := storageConsumer.DeepCopy()
		newStorageConsumer.Name = defaults.LocalStorageConsumerName
		newStorageConsumer.SetResourceVersion("")
		if err = r.reconcileStorageConsumer(ctx, storageCluster, newStorageConsumer, defaults.LocalStorageConsumerConfigMapName); err != nil {
			return reconcile.Result{}, err
		}

		if err = r.reconcileLocalStorageClient(ctx, storageCluster.Name, newStorageConsumer.UID); err != nil {
			return reconcile.Result{}, err
		}
		if err = r.reconcileStorageCluster(ctx, storageCluster, storageConsumer.UID); err != nil {
			return reconcile.Result{}, err
		}
		// Delete the old consumer
		if err = r.Client.Delete(ctx, storageConsumer); err != nil {
			return reconcile.Result{}, err
		}
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
		resourceMap := util.WrapStorageConsumerResourceMap(data)
		util.FillBackwardCompatibleConsumerConfigValues(storageCluster, string(storageConsumer.UID), resourceMap)
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
	rbdStorageRequestName := util.GetStorageRequestName(string(storageConsumer.UID), rbdClaimName)
	cephClientName := generateHashForCephClient(rbdStorageRequestName, "provisioner")
	if err := r.reconcilePre4_19CephClient(
		ctx,
		cephClientName,
		storageConsumer.Namespace,
		storageConsumer,
	); err != nil {
		return err
	}
	cephClientName = generateHashForCephClient(rbdStorageRequestName, "node")
	if err := r.reconcilePre4_19CephClient(
		ctx,
		cephClientName,
		storageConsumer.Namespace,
		storageConsumer,
	); err != nil {
		return err
	}

	rbdStorageRequest := &metav1.PartialObjectMetadata{}
	rbdStorageRequest.SetGroupVersionKind(ocsv1alpha1.GroupVersion.WithKind("StorageRequest"))
	rbdStorageRequest.Name = rbdStorageRequestName
	rbdStorageRequest.Namespace = storageConsumer.Namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(rbdStorageRequest), rbdStorageRequest); client.IgnoreNotFound(err) != nil {
		return err
	} else if rbdStorageRequest.UID != "" {
		rbdStorageRequestCopy := &metav1.PartialObjectMetadata{}
		rbdStorageRequest.DeepCopyInto(rbdStorageRequestCopy)
		rbdStorageRequestCopy.Finalizers = []string{}
		if err := r.Client.Patch(ctx, rbdStorageRequestCopy, client.MergeFrom(rbdStorageRequest)); err != nil {
			return err
		}
		if err := r.Client.Delete(ctx, rbdStorageRequest); err != nil {
			return err
		}

	}
	cephFsClaimName := util.GenerateNameForCephFilesystemStorageClass(storageCluster)
	cephFsStorageRequestName := util.GetStorageRequestName(string(storageConsumer.UID), cephFsClaimName)
	cephFsStorageRequestMd5Sum := md5.Sum([]byte(cephFsStorageRequestName))
	svgName := fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsStorageRequestMd5Sum[:16]))

	// Removing the onwerRef from svg to preserve it after deletion the StorageRequest
	if err := r.removeStorageRequestOwner(ctx, "CephFilesystemSubVolumeGroup", svgName, storageConsumer.Namespace); err != nil {
		return err
	}

	cephClientName = generateHashForCephClient(cephFsStorageRequestName, "provisioner")
	if err := r.reconcilePre4_19CephClient(
		ctx,
		cephClientName,
		storageConsumer.Namespace,
		storageConsumer,
	); err != nil {
		return err
	}
	cephClientName = generateHashForCephClient(cephFsStorageRequestName, "node")
	if err := r.reconcilePre4_19CephClient(
		ctx,
		cephClientName,
		storageConsumer.Namespace,
		storageConsumer,
	); err != nil {
		return err
	}

	cephFsStorageRequest := &metav1.PartialObjectMetadata{}
	cephFsStorageRequest.SetGroupVersionKind(ocsv1alpha1.GroupVersion.WithKind("StorageRequest"))
	cephFsStorageRequest.Name = cephFsStorageRequestName
	cephFsStorageRequest.Namespace = storageConsumer.Namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(cephFsStorageRequest), cephFsStorageRequest); client.IgnoreNotFound(err) != nil {
		return err
	} else if cephFsStorageRequest.UID != "" {
		cephFsStorageRequestCopy := &metav1.PartialObjectMetadata{}
		cephFsStorageRequest.DeepCopyInto(cephFsStorageRequestCopy)
		cephFsStorageRequestCopy.Finalizers = []string{}
		if err := r.Client.Patch(ctx, cephFsStorageRequestCopy, client.MergeFrom(cephFsStorageRequest)); err != nil {
			return err
		}
		if err := r.Client.Delete(ctx, cephFsStorageRequest); err != nil {
			return err
		}
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
			objCopy := &metav1.PartialObjectMetadata{}
			obj.DeepCopyInto(objCopy)
			objCopy.SetOwnerReferences(slices.Delete(refs, idx, idx+1))
			if err := r.Client.Patch(ctx, objCopy, client.MergeFrom(obj)); err != nil {
				return err
			}
		}
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileStorageConsumer(
	ctx context.Context,
	storageCluster *ocsv1.StorageCluster,
	storageConsumer *ocsv1alpha1.StorageConsumer,
	consumerConfigMapName string,
) error {
	if _, err := controllerutil.CreateOrUpdate(ctx, r.Client, storageConsumer, func() error {
		util.AddLabel(storageConsumer, util.CreatedAtDfVersionLabelKey, "4.18")
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
		delete(storageConsumer.GetAnnotations(), util.TicketAnnotation)
		return nil
	}); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileStorageCluster(
	ctx context.Context,
	storageCluster *ocsv1.StorageCluster,
	storageConsumerUID types.UID,
) error {
	util.AddAnnotation(
		storageCluster,
		util.BackwardCompatabilityInfoAnnotationKey,
		string(util.JsonMustMarshal(util.BackwardCompatabilityInfo{Pre4_19InternalConsumer: string(storageConsumerUID)})),
	)
	if err := r.Client.Update(ctx, storageCluster); err != nil {
		return fmt.Errorf("failed to add storage cluster compatibility mode annotation: %w", err)
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcileLocalStorageClient(
	ctx context.Context,
	storageClientName string,
	consumerUID types.UID,
) error {
	storageClient := &ocsclientv1a1.StorageClient{}
	storageClient.Name = storageClientName
	statusPatch := client.RawPatch(types.MergePatchType, util.JsonMustMarshal(
		map[string]any{
			"status": map[string]any{
				"id": string(consumerUID),
			},
		},
	))
	if err := r.Client.Status().Patch(ctx, storageClient, statusPatch); err != nil {
		return err
	}
	return nil
}

func (r *StorageConsumerUpgradeReconciler) reconcilePre4_19CephClient(
	ctx context.Context,
	name string,
	namespace string,
	storageConsumer client.Object,
) error {
	cephClient := &rookCephv1.CephClient{}
	cephClient.Name = name
	cephClient.Namespace = namespace
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(cephClient), cephClient); err != nil {
		return err
	}
	if err := controllerutil.SetOwnerReference(storageConsumer, cephClient, r.Scheme); err != nil {
		return err
	}
	util.AddLabel(cephClient, util.CsiCephUserGenerationLabelKey, "0")
	if err := r.Client.Update(ctx, cephClient); err != nil {
		return err
	}
	return nil
}

// pre419 function to generate the cephclient name
func generateHashForCephClient(storageRequestName, cephUserType string) string {
	var c struct {
		StorageRequestName string `json:"id"`
		CephUserType       string `json:"cephUserType"`
	}

	c.StorageRequestName = storageRequestName
	c.CephUserType = cephUserType

	cephClient, err := json.Marshal(c)
	if err != nil {
		panic("failed to marshal")
	}
	name := md5.Sum([]byte(cephClient))
	return hex.EncodeToString(name[:16])
}
