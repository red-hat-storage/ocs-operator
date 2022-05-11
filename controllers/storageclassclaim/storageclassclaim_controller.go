/*
Copyright 2020 Red Hat OpenShift Container Storage.

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

package storageclassclaim

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"k8s.io/klog/v2"

	"github.com/go-logr/logr"
	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/controllers/storagecluster"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	providerclient "github.com/red-hat-storage/ocs-operator/services/provider/client"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageClassClaimReconciler reconciles a StorageClassClaim object
// nolint
type StorageClassClaimReconciler struct {
	client.Client
	Scheme                       *runtime.Scheme
	log                          logr.Logger
	ctx                          context.Context
	storageConsumer              *v1alpha1.StorageConsumer
	storageCluster               *v1.StorageCluster
	storageClassClaim            *v1alpha1.StorageClassClaim
	cephBlockPool                *rookCephv1.CephBlockPool
	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientProvisioner        *rookCephv1.CephClient
	cephClientNode               *rookCephv1.CephClient
	cephResourcesByName          map[string]*v1alpha1.CephResourcesSpec
	namespace                    string
}

const (
	StorageClassClaimFinalizer  = "storageclassclaim.ocs.openshift.io"
	StorageClassClaimAnnotation = "ocs.openshift.io.storagesclassclaim"
)

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch

func (r *StorageClassClaimReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	r.log = ctrllog.FromContext(ctx, "StorageClassClaim", request)
	r.ctx = ctrllog.IntoContext(ctx, r.log)
	r.namespace = request.Namespace
	r.log.Info("Reconciling StorageClassClaim.")

	// Fetch the StorageClassClaim instance
	r.storageClassClaim = &v1alpha1.StorageClassClaim{}
	r.storageClassClaim.Name = request.Name
	r.storageClassClaim.Namespace = r.namespace

	if err := r.get(r.storageClassClaim); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageClassClaim resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageClassClaim.")
		return reconcile.Result{}, err
	}

	r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimInitializing

	storageClusterList := &v1.StorageClusterList{}
	if err := r.list(storageClusterList, client.InNamespace(r.namespace)); err != nil {
		return reconcile.Result{}, err
	}

	switch l := len(storageClusterList.Items); {
	case l == 0:
		return reconcile.Result{}, fmt.Errorf("no StorageCluster found")
	case l != 1:
		return reconcile.Result{}, fmt.Errorf("multiple StorageCluster found")
	}
	r.storageCluster = &storageClusterList.Items[0]

	var result reconcile.Result
	var reconcileError error
	if storagecluster.IsOCSConsumerMode(r.storageCluster) {

		// StorageCluster checks for required fields.
		switch storageCluster := r.storageCluster; {
		case storageCluster.Status.ExternalStorage.ConsumerID == "":
			return reconcile.Result{}, fmt.Errorf("no external storage consumer id found on the " +
				"StorageCluster status, cannot determine mode")
		case storageCluster.Spec.ExternalStorage.StorageProviderEndpoint == "":
			return reconcile.Result{}, fmt.Errorf("no external storage provider endpoint found on the " +
				"StorageCluster spec, cannot determine mode")
		}
		result, reconcileError = r.reconcileConsumerPhases()
	} else {
		result, reconcileError = r.reconcileProviderPhases()
	}

	// Apply status changes to the StorageClassClaim
	statusError := r.Client.Status().Update(r.ctx, r.storageClassClaim)
	if statusError != nil {
		r.log.Info("Failed to update StorageClassClaim status.")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	}

	if statusError != nil {
		return result, statusError
	}

	return result, nil
}

func (r *StorageClassClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.StorageClassClaim{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&rookCephv1.CephBlockPool{}).
		Owns(&rookCephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&rookCephv1.CephClient{}).
		Owns(&storagev1.StorageClass{}).
		Complete(r)
}

func (r *StorageClassClaimReconciler) reconcileConsumerPhases() (reconcile.Result, error) {
	r.log.Info("Running StorageClassClaim controller in Consumer Mode")

	providerClient, err := providerclient.NewProviderClient(
		r.ctx,
		r.storageCluster.Spec.ExternalStorage.StorageProviderEndpoint,
		10*time.Second,
	)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Close client-side connections.
	defer providerClient.Close()

	if r.storageClassClaim.GetDeletionTimestamp().IsZero() {

		// TODO: Phases do not have checks at the moment, in order to make them more predictable and less error-prone, at the expense of increased computation cost.
		// Validation phase.
		r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimValidating

		// If a StorageClass already exists:
		// 	StorageClassClaim passes validation and is promoted to the configuring phase if:
		//  * the StorageClassClaim has the same type as the StorageClass.
		// 	* the StorageClassClaim has no encryption method specified when the type is filesystem.
		// 	* the StorageClassClaim has a blockpool type and:
		// 		 * the StorageClassClaim has an encryption method specified.
		// 	  * the StorageClassClaim has the same encryption method as the StorageClass.
		// 	StorageClassClaim fails validation and falls back to a failed phase indefinitely (no reconciliation happens).
		existing := &storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: r.storageClassClaim.Name,
			},
		}
		if err = r.get(existing); err == nil {
			annotation, found := existing.GetAnnotations()[StorageClassClaimAnnotation]
			if !found {
				r.log.Error(fmt.Errorf("storageClass %q does not belong to any storageclass claim", existing.Name), "StorageClassClaim validation failed.")
				r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimFailed
				return reconcile.Result{}, nil
			}
			claimNamespacedName := strings.Split(annotation, "/")
			if claimNamespacedName[0] != r.storageClassClaim.Namespace {
				r.log.Error(fmt.Errorf("storageClass belongs to %q storageclass claim in %q namespace", r.storageClassClaim.Name, r.storageClassClaim.Namespace), "StorageClassClaim validation failed.")
				r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimFailed
				return reconcile.Result{}, nil
			}

			sccType := r.storageClassClaim.Spec.Type
			sccEncryptionMethod := r.storageClassClaim.Spec.EncryptionMethod
			_, scIsFSType := existing.Parameters["fsName"]
			scEncryptionMethod, scHasEncryptionMethod := existing.Parameters["encryptionMethod"]
			if !((sccType == "filesystem" && scIsFSType && !scHasEncryptionMethod) ||
				(sccType == "blockpool" && !scIsFSType && sccEncryptionMethod == scEncryptionMethod)) {
				r.log.Error(fmt.Errorf("storageClassClaim is not compatible with existing StorageClass"),
					"StorageClassClaim validation failed.")
				r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimFailed
				return reconcile.Result{}, nil
			}
		} else if err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("failed to get StorageClass [%v]: %s", existing.ObjectMeta, err)
		}

		// Configuration phase.
		r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimConfiguring

		// Check if finalizers are present, if not, add them.
		if !contains(r.storageClassClaim.GetFinalizers(), StorageClassClaimFinalizer) {
			storageClassClaimRef := klog.KRef(r.storageClassClaim.Name, r.storageClassClaim.Namespace)
			r.log.Info("Finalizer not found for StorageClassClaim. Adding finalizer.", "StorageClassClaim", storageClassClaimRef)
			r.storageClassClaim.SetFinalizers(append(r.storageClassClaim.GetFinalizers(), StorageClassClaimFinalizer))
			if err := r.update(r.storageClassClaim); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to update StorageClassClaim [%v] with finalizer: %s", storageClassClaimRef, err)
			}
		}

		// storageClassClaimStorageType is the storage type of the StorageClassClaim
		var storageClassClaimStorageType providerclient.StorageType
		switch r.storageClassClaim.Spec.Type {
		case "blockpool":
			storageClassClaimStorageType = providerclient.StorageTypeBlockpool
		case "sharedfilesystem":
			storageClassClaimStorageType = providerclient.StorageTypeSharedfilesystem
		default:
			return reconcile.Result{}, fmt.Errorf("unsupported storage type: %s", r.storageClassClaim.Spec.Type)
		}

		// Call the `FulfillStorageClassClaim` service on the provider server with StorageClassClaim as a request message.
		_, err = providerClient.FulfillStorageClassClaim(
			r.ctx,
			r.storageCluster.Status.ExternalStorage.ConsumerID,
			r.storageClassClaim.Name,
			r.storageClassClaim.Spec.EncryptionMethod,
			storageClassClaimStorageType,
		)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to initiate fulfillment of StorageClassClaim: %v", err)
		}

		// Call the `GetStorageClassClaimConfig` service on the provider server with StorageClassClaim as a request message.
		response, err := providerClient.GetStorageClassClaimConfig(
			r.ctx,
			r.storageCluster.Status.ExternalStorage.ConsumerID,
			r.storageClassClaim.Name,
		)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to get StorageClassClaim config: %v", err)
		}
		resources := response.ExternalResource
		if resources == nil {
			return reconcile.Result{}, fmt.Errorf("no configuration data recieved")
		}

		// Go over the received objects and operate on them accordingly.
		for _, resource := range resources {
			data := map[string]string{}
			err = json.Unmarshal(resource.Data, &data)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to unmarshal StorageClassClaim configuration response: %v", err)
			}

			// Create the received resources, if necessary.
			switch resource.Kind {
			case "Secret":
				secret := &corev1.Secret{}
				secret.Name = resource.Name
				secret.Namespace = r.storageClassClaim.Namespace
				_, err = controllerutil.CreateOrUpdate(r.ctx, r.Client, secret, func() error {
					err := r.own(secret)
					if err != nil {
						return fmt.Errorf("failed to own Secret: %v", err)
					}
					if secret.Data == nil {
						secret.Data = map[string][]byte{}
					}
					for k, v := range data {
						secret.Data[k] = []byte(v)
					}
					return nil
				})
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to create or update secret %v: %s", secret, err)
				}
			case "CephFilesystemSubVolumeGroup":
				subVolumeGroup := &rookCephv1.CephFilesystemSubVolumeGroup{ObjectMeta: metav1.ObjectMeta{
					Name:      resource.Name,
					Namespace: r.storageClassClaim.Namespace,
				}}
				_, err = ctrl.CreateOrUpdate(context.TODO(), r.Client, subVolumeGroup, func() error {
					if err := r.own(subVolumeGroup); err != nil {
						return err
					}
					subVolumeGroup.Spec = rookCephv1.CephFilesystemSubVolumeGroupSpec{
						FilesystemName: data["filesystemName"],
					}
					return nil
				})
				if err != nil {
					r.log.Error(err, "Could not create CephFilesystemSubVolumeGroup.", "CephFilesystemSubVolumeGroup", klog.KRef(subVolumeGroup.Namespace, subVolumeGroup.Name))
					return reconcile.Result{}, err
				}
			case "StorageClass":
				var storageClass *storagev1.StorageClass
				data["csi.storage.k8s.io/provisioner-secret-namespace"] = r.storageClassClaim.Namespace
				data["csi.storage.k8s.io/node-stage-secret-namespace"] = r.storageClassClaim.Namespace
				data["csi.storage.k8s.io/controller-expand-secret-namespace"] = r.storageClassClaim.Namespace

				if resource.Name == "cephfs" {
					storageClass = r.getCephFSStorageClass(data)
				} else if resource.Name == "ceph-rbd" {
					storageClass = r.getCephRBDStorageClass(data)
				}
				err = r.createOrReplaceStorageClass(storageClass)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to create or update StorageClass: %s", err)
				}
			}
		}

		// Readiness phase.
		// Update the StorageClassClaim status.
		r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimReady

		// Initiate deletion phase if the StorageClassClaim exists.
	} else if r.storageClassClaim.UID != "" {

		// Deletion phase.
		// Update the StorageClassClaim status.
		r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimDeleting

		// Delete StorageClass.
		// Make sure there are no StorageClass consumers left.
		// Check if StorageClass is in use, if yes, then fail.
		// Wait until all PVs using the StorageClass under deletion are removed.
		// Check for any PVs using the StorageClass.
		pvList := corev1.PersistentVolumeList{}
		err := r.list(&pvList)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to list PersistentVolumes: %s", err)
		}
		for i := range pvList.Items {
			pv := &pvList.Items[i]
			if pv.Spec.StorageClassName == r.storageClassClaim.Name {
				return reconcile.Result{}, fmt.Errorf("StorageClass %s is still in use by one or more PV(s)",
					r.storageClassClaim.Name)
			}
		}

		// Call `RevokeStorageClassClaim` service on the provider server with StorageClassClaim as a request message.
		// Check if StorageClassClaim is still exists (it might have been manually removed during the StorageClass
		// removal above).
		_, err = providerClient.RevokeStorageClassClaim(
			r.ctx,
			r.storageCluster.Status.ExternalStorage.ConsumerID,
			r.storageClassClaim.Name,
		)
		if err != nil {
			return reconcile.Result{}, err
		}

		storageClass := &storagev1.StorageClass{}
		storageClass.Name = r.storageClassClaim.Name
		if err = r.get(storageClass); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("failed to get StorageClass %s: %s", storageClass.Name, err)
		}
		if storageClass.UID != "" {

			if err = r.delete(storageClass); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to delete StorageClass %s: %s", storageClass.Name, err)
			}
		} else {
			r.log.Info("StorageClass already deleted.")
		}
		if contains(r.storageClassClaim.GetFinalizers(), StorageClassClaimFinalizer) {
			r.storageClassClaim.Finalizers = remove(r.storageClassClaim.Finalizers, StorageClassClaimFinalizer)
			if err := r.update(r.storageClassClaim); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to remove finalizer from storageClassClaim: %s", err)
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) reconcileProviderPhases() (reconcile.Result, error) {
	r.log.Info("Running StorageClassClaim controller in Converged/Provider Mode")

	r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimInitializing

	gvk, err := apiutil.GVKForObject(&v1alpha1.StorageConsumer{}, r.Client.Scheme())
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to get gvk for consumer  %w", err)
	}
	// reading storageConsumer Name from storageClassClaim ownerReferences
	ownerRefs := r.storageClassClaim.GetOwnerReferences()
	for i := range ownerRefs {
		if ownerRefs[i].Kind == gvk.Kind {
			r.storageConsumer = &v1alpha1.StorageConsumer{}
			r.storageConsumer.Name = ownerRefs[i].Name
			r.storageConsumer.Namespace = r.storageCluster.Namespace
			break
		}
	}
	if r.storageConsumer == nil {
		return reconcile.Result{}, fmt.Errorf("no storage consumer owner ref on the storage class claim")
	}

	if err := r.get(r.storageConsumer); err != nil {
		return reconcile.Result{}, err
	}

	if r.storageClassClaim.Spec.Type == "blockpool" {
		r.cephBlockPool = &rookCephv1.CephBlockPool{}
		r.cephBlockPool.Name = fmt.Sprintf("cephblockpool-%s", r.storageConsumer.Name)
		r.cephBlockPool.Namespace = r.namespace
		r.cephBlockPool.Labels = map[string]string{
			controllers.StorageConsumerNameLabel: r.storageConsumer.Name,
		}

	} else if r.storageClassClaim.Spec.Type == "sharedfilesystem" {
		r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
		r.cephFilesystemSubVolumeGroup.Name = fmt.Sprintf("cephfilesystemsubvolumegroup-%s", r.storageConsumer.Name)
		r.cephFilesystemSubVolumeGroup.Namespace = r.namespace
	}

	r.cephClientProvisioner = &rookCephv1.CephClient{}
	r.cephClientProvisioner.Name = controllers.GenerateHashForCephClient(r.storageClassClaim.Name, "provisioner")
	r.cephClientProvisioner.Namespace = r.namespace

	r.cephClientNode = &rookCephv1.CephClient{}
	r.cephClientNode.Name = controllers.GenerateHashForCephClient(r.storageClassClaim.Name, "node")
	r.cephClientNode.Namespace = r.namespace

	r.cephResourcesByName = map[string]*v1alpha1.CephResourcesSpec{}

	for _, cephResourceSpec := range r.storageClassClaim.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimCreating

	if r.storageClassClaim.GetDeletionTimestamp().IsZero() {
		if r.storageClassClaim.Spec.Type == "blockpool" {

			if err := r.reconcileCephClientRBDProvisioner(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephClientRBDNode(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephBlockPool(); err != nil {
				return reconcile.Result{}, err
			}

		} else if r.storageClassClaim.Spec.Type == "sharedfilesystem" {
			if err := r.reconcileCephClientCephFSProvisioner(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephClientCephFSNode(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephFilesystemSubVolumeGroup(); err != nil {
				return reconcile.Result{}, err
			}
		}
		cephResourcesReady := true
		for _, cephResource := range r.storageClassClaim.Status.CephResources {
			if cephResource.Phase != "Ready" {
				cephResourcesReady = false
				break
			}
		}

		if cephResourcesReady {
			r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimReady
		}

	} else {
		r.storageClassClaim.Status.Phase = v1alpha1.StorageClassClaimDeleting
	}
	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) getCephFSStorageClass(data map[string]string) *storagev1.StorageClass {
	pvReclaimPolicy := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	storageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.storageClassClaim.Name,
			Annotations: map[string]string{
				"description": "Provides RWO and RWX Filesystem volumes",
			},
		},
		ReclaimPolicy:        &pvReclaimPolicy,
		AllowVolumeExpansion: &allowVolumeExpansion,
		Provisioner:          fmt.Sprintf("%s.cephfs.csi.ceph.com", r.storageCluster.Namespace),
		Parameters:           data,
	}
	return storageClass
}

func (r *StorageClassClaimReconciler) getCephRBDStorageClass(data map[string]string) *storagev1.StorageClass {
	pvReclaimPolicy := corev1.PersistentVolumeReclaimDelete
	allowVolumeExpansion := true
	storageClass := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.storageClassClaim.Name,
			Annotations: map[string]string{
				"description": "Provides RWO Filesystem volumes, and RWO and RWX Block volumes",
			},
		},
		ReclaimPolicy:        &pvReclaimPolicy,
		AllowVolumeExpansion: &allowVolumeExpansion,
		Provisioner:          fmt.Sprintf("%s.rbd.csi.ceph.com", r.storageCluster.Namespace),
		Parameters:           data,
	}
	return storageClass
}

func (r *StorageClassClaimReconciler) createOrReplaceStorageClass(storageClass *storagev1.StorageClass) error {
	existing := &storagev1.StorageClass{}
	existing.Name = r.storageClassClaim.Name

	if err := r.get(existing); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get StorageClass: %v", err)
	}

	// If present then compare the existing StorageClass with the received StorageClass, and only proceed if they differ.
	if reflect.DeepEqual(existing.Parameters, storageClass.Parameters) {
		return nil
	}

	// StorageClass already exists, but parameters have changed. Delete the existing StorageClass and create a new one.
	if existing.UID != "" {

		// Since we have to update the existing StorageClass, so we will delete the existing StorageClass and create a new one.
		r.log.Info("StorageClass needs to be updated, deleting it.", "StorageClass", klog.KRef(storageClass.Namespace, existing.Name))

		// Delete the StorageClass.
		err := r.delete(existing)
		if err != nil {
			r.log.Error(err, "Failed to delete StorageClass.", "StorageClass", klog.KRef(storageClass.Namespace, existing.Name))
			return err
		}
	}
	r.log.Info("Creating StorageClass.", "StorageClass", klog.KRef(storageClass.Namespace, existing.Name))
	err := r.Client.Create(r.ctx, storageClass)
	if err != nil {
		return fmt.Errorf("failed to create StorageClass: %v", err)
	}
	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephBlockPool() error {

	failureDomain := r.storageCluster.Status.FailureDomain

	capacity := r.storageConsumer.Spec.Capacity.String()

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephBlockPool, func() error {
		if err := r.own(r.cephBlockPool); err != nil {
			return err
		}
		deviceSetList := r.storageCluster.Spec.StorageDeviceSets
		var deviceSet *v1.StorageDeviceSet = nil
		for i := range deviceSetList {
			ds := &deviceSetList[i]
			if ds.Name == "default" {
				deviceSet = ds
				break
			}
		}
		if deviceSet == nil {
			return fmt.Errorf("Could not find  device set named default in Storage cluster")
		}
		pgUnitSize := util.GetPGBaseUnitSize(deviceSet.Count)
		r.cephBlockPool.Labels[controllers.StorageConsumerNameLabel] = r.storageConsumer.Name
		r.cephBlockPool.Spec = rookCephv1.NamedBlockPoolSpec{
			PoolSpec: rookCephv1.PoolSpec{
				FailureDomain: failureDomain,
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     3,
					ReplicasPerFailureDomain: 1,
				},
				Parameters: map[string]string{
					"target_size_ratio": ".49",
					"pg_autoscale_mode": "off",
					"pg_num":            strconv.Itoa(pgUnitSize),
					"pgp_num":           strconv.Itoa(pgUnitSize),
				},
				Quotas: rookCephv1.QuotaSpec{
					MaxSize: &capacity,
				},
			},
		}
		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephBlockPool.",
			"CephBlockPool",
			klog.KRef(r.cephBlockPool.Namespace, r.cephBlockPool.Name),
		)
		return err
	}

	cephClients := map[string]string{
		"provisioner": r.cephClientProvisioner.Name,
		"node":        r.cephClientNode.Name,
	}
	phase := ""
	if r.cephBlockPool.Status != nil {
		phase = string(r.cephBlockPool.Status.Phase)
	}

	r.setCephResourceStatus(r.cephBlockPool.Name, "CephBlockPool", phase, cephClients)

	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephFilesystemSubVolumeGroup() error {

	cephFilesystemList := rookCephv1.CephFilesystemList{}
	if err := r.list(&cephFilesystemList, client.InNamespace(r.namespace)); err != nil {
		return fmt.Errorf("error fetching CephFilesystemList. %+v", err)
	}

	var cephFileSystemName string
	availableCephFileSystems := len(cephFilesystemList.Items)
	if availableCephFileSystems == 0 {
		return fmt.Errorf("no CephFileSystem found in the cluster")
	}
	if availableCephFileSystems > 1 {
		klog.Warningf("More than one CephFileSystem found in the cluster, selecting the first one")
	}

	cephFileSystemName = cephFilesystemList.Items[0].Name

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephFilesystemSubVolumeGroup, func() error {
		if err := r.own(r.cephFilesystemSubVolumeGroup); err != nil {
			return err
		}

		r.cephFilesystemSubVolumeGroup.Spec = rookCephv1.CephFilesystemSubVolumeGroupSpec{
			FilesystemName: cephFileSystemName,
		}
		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephFilesystemSubVolumeGroup.",
			"CephFilesystemSubVolumeGroup",
			klog.KRef(r.cephFilesystemSubVolumeGroup.Namespace, r.cephFilesystemSubVolumeGroup.Name),
		)
		return err
	}

	cephClients := map[string]string{
		"provisioner": r.cephClientProvisioner.Name,
		"node":        r.cephClientNode.Name,
	}
	phase := ""
	if r.cephFilesystemSubVolumeGroup.Status != nil {
		phase = string(r.cephFilesystemSubVolumeGroup.Status.Phase)
	}

	r.setCephResourceStatus(r.cephFilesystemSubVolumeGroup.Name, "CephFilesystemSubVolumeGroup", phase, cephClients)

	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephClientRBDProvisioner() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientProvisioner, func() error {
		if err := r.own(r.cephClientProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.storageClassClaim.Name, "rbd", "provisioner")
		r.cephClientProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s", r.cephBlockPool.Name),
			},
		}
		return nil
	})

	if err != nil {
		r.log.Error(err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientProvisioner.Namespace, r.cephClientProvisioner.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientProvisioner.Status != nil {
		phase = string(r.cephClientProvisioner.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientProvisioner.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephClientRBDNode() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientNode, func() error {
		if err := r.own(r.cephClientNode); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientNode, r.storageClassClaim.Name, "rbd", "node")
		r.cephClientNode.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s", r.cephBlockPool.Name),
			},
		}

		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientNode.Namespace, r.cephClientNode.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientNode.Status != nil {
		phase = string(r.cephClientNode.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientNode.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephClientCephFSProvisioner() error {

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientProvisioner, func() error {
		if err := r.own(r.cephClientProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.storageClassClaim.Name, "cephfs", "provisioner")
		r.cephClientProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "allow r",
				"mgr": "allow rw",
				"mds": fmt.Sprintf("allow rw path=/volumes/%s", r.cephFilesystemSubVolumeGroup.Name),
				"osd": "allow rw tag cephfs metadata=*",
			},
		}
		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientProvisioner.Namespace, r.cephClientProvisioner.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientProvisioner.Status != nil {
		phase = string(r.cephClientProvisioner.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientProvisioner.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageClassClaimReconciler) reconcileCephClientCephFSNode() error {

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientNode, func() error {
		if err := r.own(r.cephClientNode); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientNode, r.storageClassClaim.Name, "cephfs", "node")
		r.cephClientNode.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "allow r",
				"mgr": "allow rw",
				"osd": "allow rw tag cephfs *=*",
				"mds": fmt.Sprintf("allow rw path=/volumes/%s", r.cephFilesystemSubVolumeGroup.Name),
			},
		}
		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientNode.Namespace, r.cephClientNode.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientNode.Status != nil {
		phase = string(r.cephClientNode.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientNode.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageClassClaimReconciler) setCephResourceStatus(name string, kind string, phase string, cephClients map[string]string) {

	cephResourceSpec := r.cephResourcesByName[name]

	if cephResourceSpec == nil {
		cephResourceSpec = &v1alpha1.CephResourcesSpec{
			Name:        name,
			Kind:        kind,
			CephClients: cephClients,
		}
		r.storageClassClaim.Status.CephResources = append(r.storageClassClaim.Status.CephResources, cephResourceSpec)
		r.cephResourcesByName[name] = cephResourceSpec
	}

	cephResourceSpec.Phase = phase
}

func addStorageRelatedAnnotations(obj client.Object, storageClassClaimName, storageClaim, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[StorageClassClaimAnnotation] = storageClassClaimName
	annotations[controllers.StorageClaimAnnotation] = storageClaim
	annotations[controllers.StorageCephUserTypeAnnotation] = cephUserType
}

func (r *StorageClassClaimReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageClassClaimReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}

func (r *StorageClassClaimReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *StorageClassClaimReconciler) delete(obj client.Object) error {
	if err := r.Client.Delete(r.ctx, obj); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

func (r *StorageClassClaimReconciler) own(resource metav1.Object) error {
	// Ensure StorageClassClaim ownership on a resource
	return controllerutil.SetOwnerReference(r.storageClassClaim, resource, r.Scheme)
}

func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// Removes a given string from a slice and returns the new slice
func remove(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}
