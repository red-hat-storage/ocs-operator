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
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/controllers/storagecluster"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"
	providerclient "github.com/red-hat-storage/ocs-operator/services/provider/client"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

const (
	storageClassEncryptionParamKey = "encryptionKMSID"
)

// StorageClassClaimReconciler reconciles a StorageClassClaim object
// nolint:revive
type StorageClassClaimReconciler struct {
	client.Client
	cache.Cache
	Scheme            *runtime.Scheme
	OperatorNamespace string

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
	storageProfile               *v1.StorageProfile
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch

func (r *StorageClassClaimReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if ok := r.Cache.WaitForCacheSync(ctx); !ok {
		return reconcile.Result{}, fmt.Errorf("cache sync failed")
	}

	r.log = ctrllog.FromContext(ctx, "StorageClassClaim", request)
	r.ctx = ctrllog.IntoContext(ctx, r.log)
	r.log.Info("Reconciling StorageClassClaim.")

	// Fetch the StorageClassClaim instance
	r.storageClassClaim = &v1alpha1.StorageClassClaim{}
	r.storageClassClaim.Name = request.Name
	r.storageClassClaim.Namespace = request.Namespace

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
	if err := r.list(storageClusterList, client.InNamespace(r.OperatorNamespace)); err != nil {
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
	enqueueStorageConsumerRequest := handler.EnqueueRequestsFromMapFunc(
		func(obj client.Object) []reconcile.Request {
			annotations := obj.GetAnnotations()
			if annotation, found := annotations[v1alpha1.StorageClassClaimAnnotation]; found {
				parts := strings.Split(annotation, "/")
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Namespace: parts[0],
						Name:      parts[1],
					},
				}}
			}
			return []reconcile.Request{}
		})
	// As we are not setting the Controller OwnerReference on the ceph
	// resources we are creating as part of the StorageClassClaim, we need to
	// set IsController to false to get Reconcile Request of StorageClassClaim
	// for the owned ceph resources updates.
	enqueueForNonControllerOwner := &handler.EnqueueRequestForOwner{OwnerType: &v1alpha1.StorageClassClaim{}, IsController: false}
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.StorageClassClaim{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Watches(&source.Kind{Type: &rookCephv1.CephBlockPool{}}, enqueueForNonControllerOwner).
		Watches(&source.Kind{Type: &rookCephv1.CephFilesystemSubVolumeGroup{}}, enqueueForNonControllerOwner).
		Watches(&source.Kind{Type: &rookCephv1.CephClient{}}, enqueueForNonControllerOwner).
		Watches(&source.Kind{Type: &storagev1.StorageClass{}}, enqueueStorageConsumerRequest).
		Watches(&source.Kind{Type: &snapapi.VolumeSnapshotClass{}}, enqueueStorageConsumerRequest).
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
			sccType := r.storageClassClaim.Spec.Type
			sccEncryptionMethod := r.storageClassClaim.Spec.EncryptionMethod
			_, scIsFSType := existing.Parameters["fsName"]

			scEncryptionMethod, scHasEncryptionMethod := existing.Parameters[storageClassEncryptionParamKey]

			validStorageClassConfig := true
			if sccType == "sharedfilesystem" {
				// valdiate that the request is not asking to change the sc type
				if !scIsFSType {
					validStorageClassConfig = false
				}

				// validate that encryption is disabled
				if scHasEncryptionMethod {
					validStorageClassConfig = false
				}
			} else if sccType == "blockpool" {
				// valdiate that the request is not asking to change the sc type
				if scIsFSType {
					validStorageClassConfig = false
				}

				// validate that the request is not asking to change encryption type
				if sccEncryptionMethod != scEncryptionMethod {
					validStorageClassConfig = false
				}
			}

			if !validStorageClassConfig {
				r.log.Error(fmt.Errorf("storageClassClaim %s is not compatible with existing StorageClass.%t %s %s", sccType, scHasEncryptionMethod, sccEncryptionMethod, scEncryptionMethod),
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
		if !contains(r.storageClassClaim.GetFinalizers(), v1alpha1.StorageClassClaimFinalizer) {
			storageClassClaimRef := klog.KRef(r.storageClassClaim.Name, r.storageClassClaim.Namespace)
			r.log.Info("Finalizer not found for StorageClassClaim. Adding finalizer.", "StorageClassClaim", storageClassClaimRef)
			r.storageClassClaim.SetFinalizers(append(r.storageClassClaim.GetFinalizers(), v1alpha1.StorageClassClaimFinalizer))
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
			storageClassClaimStorageType,
			r.storageClassClaim.Spec.StorageProfile,
			r.storageClassClaim.Spec.EncryptionMethod,
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
			return reconcile.Result{}, fmt.Errorf("no configuration data received")
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
				addAnnotation(storageClass, v1alpha1.StorageClassClaimAnnotation, r.getNamespacedName())
				err = r.createOrReplaceStorageClass(storageClass)
				if err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to create or update StorageClass: %s", err)
				}

			case "VolumeSnapshotClass":
				var volumeSnapshotClass *snapapi.VolumeSnapshotClass
				data["csi.storage.k8s.io/snapshotter-secret-namespace"] = r.storageClassClaim.Namespace

				if resource.Name == "cephfs" {
					volumeSnapshotClass = r.getCephFSVolumeSnapshotClass(data)
				} else if resource.Name == "ceph-rbd" {
					volumeSnapshotClass = r.getCephRBDVolumeSnapshotClass(data)
				}
				addAnnotation(volumeSnapshotClass, v1alpha1.StorageClassClaimAnnotation, r.getNamespacedName())
				if err := r.createOrReplaceVolumeSnapshotClass(volumeSnapshotClass); err != nil {
					return reconcile.Result{}, fmt.Errorf("failed to create or update VolumeSnapshotClass: %s", err)
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

		volumeSnapshotClass := &snapapi.VolumeSnapshotClass{}
		volumeSnapshotClass.Name = r.storageClassClaim.Name
		if err = r.get(volumeSnapshotClass); err != nil && !errors.IsNotFound(err) {
			return reconcile.Result{}, fmt.Errorf("failed to get VolumeSnapshotClass %s: %s", volumeSnapshotClass.Name, err)
		}
		if volumeSnapshotClass.UID != "" {
			if err = r.delete(volumeSnapshotClass); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to delete VolumeSnapshotClass %s: %s", volumeSnapshotClass.Name, err)
			}
		} else {
			r.log.Info("VolumeSnapshotClass already deleted", "Name", volumeSnapshotClass.Name)
		}

		if contains(r.storageClassClaim.GetFinalizers(), v1alpha1.StorageClassClaimFinalizer) {
			r.storageClassClaim.Finalizers = remove(r.storageClassClaim.Finalizers, v1alpha1.StorageClassClaimFinalizer)
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
			r.storageConsumer.Namespace = r.OperatorNamespace
			break
		}
	}
	if r.storageConsumer == nil {
		return reconcile.Result{}, fmt.Errorf("no storage consumer owner ref on the storage class claim")
	}

	if err := r.get(r.storageConsumer); err != nil {
		return reconcile.Result{}, err
	}

	// check claim status already contains the name of the resource. if not, add it.
	if r.storageClassClaim.Spec.Type == "blockpool" {
		r.cephBlockPool = &rookCephv1.CephBlockPool{}
		r.cephBlockPool.Namespace = r.OperatorNamespace
		for _, res := range r.storageClassClaim.Status.CephResources {
			if res.Kind == "CephBlockPool" {
				r.cephBlockPool.Name = res.Name
				break
			}
		}
		if r.cephBlockPool.Name == "" {
			r.cephBlockPool.Name = fmt.Sprintf("cephblockpool-%s-%s", r.storageConsumer.Name, generateUUID())
		}

	} else if r.storageClassClaim.Spec.Type == "sharedfilesystem" {
		r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
		r.cephFilesystemSubVolumeGroup.Namespace = r.OperatorNamespace
		for _, res := range r.storageClassClaim.Status.CephResources {
			if res.Kind == "CephFilesystemSubVolumeGroup" {
				r.cephFilesystemSubVolumeGroup.Name = res.Name
				break
			}
		}
		if r.cephFilesystemSubVolumeGroup.Name == "" {
			r.cephFilesystemSubVolumeGroup.Name = fmt.Sprintf("cephfilesystemsubvolumegroup-%s-%s", r.storageConsumer.Name, generateUUID())
		}
	}

	profileName := r.storageClassClaim.Spec.StorageProfile
	if profileName == "" {
		profileName = r.storageCluster.Spec.DefaultStorageProfile
	}

	for i := range r.storageCluster.Spec.StorageProfiles {
		profile := &r.storageCluster.Spec.StorageProfiles[i]
		if profile.Name == profileName {
			r.storageProfile = profile
			break
		}
	}

	if r.storageProfile == nil {
		return reconcile.Result{}, fmt.Errorf("no storage profile definition found for storage profile %s", profileName)
	}

	r.cephClientProvisioner = &rookCephv1.CephClient{}
	r.cephClientProvisioner.Name = controllers.GenerateHashForCephClient(r.storageClassClaim.Name, "provisioner")
	r.cephClientProvisioner.Namespace = r.OperatorNamespace

	r.cephClientNode = &rookCephv1.CephClient{}
	r.cephClientNode.Name = controllers.GenerateHashForCephClient(r.storageClassClaim.Name, "node")
	r.cephClientNode.Namespace = r.OperatorNamespace

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
			Name:      r.storageClassClaim.Name,
			Namespace: r.storageClassClaim.Namespace,
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
			Name:      r.storageClassClaim.Name,
			Namespace: r.storageClassClaim.Namespace,
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

func (r *StorageClassClaimReconciler) getCephFSVolumeSnapshotClass(data map[string]string) *snapapi.VolumeSnapshotClass {
	volumesnapshotclass := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.storageClassClaim.Name,
		},
		Driver:         fmt.Sprintf("%s.cephfs.csi.ceph.com", r.storageCluster.Namespace),
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
		Parameters:     data,
	}
	return volumesnapshotclass
}

func (r *StorageClassClaimReconciler) getCephRBDVolumeSnapshotClass(data map[string]string) *snapapi.VolumeSnapshotClass {
	volumesnapshotclass := &snapapi.VolumeSnapshotClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: r.storageClassClaim.Name,
		},
		Driver:         fmt.Sprintf("%s.rbd.csi.ceph.com", r.storageCluster.Namespace),
		DeletionPolicy: snapapi.VolumeSnapshotContentDelete,
		Parameters:     data,
	}
	return volumesnapshotclass
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

func (r *StorageClassClaimReconciler) createOrReplaceVolumeSnapshotClass(volumeSnapshotClass *snapapi.VolumeSnapshotClass) error {
	existing := &snapapi.VolumeSnapshotClass{}
	existing.Name = r.storageClassClaim.Name

	if err := r.get(existing); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get VolumeSnapshotClass: %v", err)
	}

	// If present then compare the existing VolumeSnapshotClass parameters with
	// the received VolumeSnapshotClass parameters, and only proceed if they differ.
	if reflect.DeepEqual(existing.Parameters, volumeSnapshotClass.Parameters) {
		return nil
	}

	// VolumeSnapshotClass already exists, but parameters have changed. Delete the existing VolumeSnapshotClass and create a new one.
	if existing.UID != "" {
		// Since we have to update the existing VolumeSnapshotClass, so we will delete the existing VolumeSnapshotClass and create a new one.
		r.log.Info("VolumeSnapshotClass needs to be updated, deleting it.", "Name", existing.Name)

		// Delete the VolumeSnapshotClass.
		if err := r.delete(existing); err != nil {
			r.log.Error(err, "Failed to delete VolumeSnapshotClass.", "Name", existing.Name)
			return err
		}
	}
	r.log.Info("Creating VolumeSnapshotClass.", "Name", existing.Name)
	if err := r.Client.Create(r.ctx, volumeSnapshotClass); err != nil {
		return fmt.Errorf("failed to create VolumeSnapshotClass: %v", err)
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
		deviceClass := r.storageProfile.DeviceClass
		deviceSetList := r.storageCluster.Spec.StorageDeviceSets
		var deviceSet *v1.StorageDeviceSet
		for i := range deviceSetList {
			ds := &deviceSetList[i]
			// get the required deviceSetName of the profile
			if deviceClass == ds.DeviceClass {
				deviceSet = ds
				break
			}
		}

		if deviceSet == nil {
			return fmt.Errorf("could not find device set definition named %s in storagecluster", deviceClass)
		}

		addLabel(r.cephBlockPool, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)

		r.cephBlockPool.Spec = rookCephv1.NamedBlockPoolSpec{
			PoolSpec: rookCephv1.PoolSpec{
				FailureDomain: failureDomain,
				DeviceClass:   deviceClass,
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     3,
					ReplicasPerFailureDomain: 1,
				},
				Parameters: r.storageProfile.BlockPoolConfiguration.Parameters,
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

	cephFilesystem := rookCephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cephfilesystem", r.storageCluster.Name),
			Namespace: r.storageCluster.Namespace,
		},
	}
	if err := r.get(&cephFilesystem); err != nil {
		return fmt.Errorf("error fetching CephFilesystem. %+v", err)
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephFilesystemSubVolumeGroup, func() error {
		if err := r.own(r.cephFilesystemSubVolumeGroup); err != nil {
			return err
		}
		deviceClass := r.storageProfile.DeviceClass
		dataPool := &rookCephv1.NamedPoolSpec{}
		for i := range cephFilesystem.Spec.DataPools {
			if cephFilesystem.Spec.DataPools[i].DeviceClass == deviceClass {
				dataPool = &cephFilesystem.Spec.DataPools[i]
				break
			}
		}
		if dataPool == nil {
			return fmt.Errorf("no CephFileSystem found in the cluster for storage profile %s", r.storageClassClaim.Spec.StorageProfile)
		}

		addLabel(r.cephFilesystemSubVolumeGroup, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)
		// This label is required to set the dataPool on the CephFS
		// storageclass so that each PVC created from CephFS storageclass can
		// use correct dataPool backed by deviceclass.
		addLabel(r.cephFilesystemSubVolumeGroup, v1alpha1.CephFileSystemDataPoolLabel, fmt.Sprintf("%s-%s", cephFilesystem.Name, dataPool.Name))

		r.cephFilesystemSubVolumeGroup.Spec = rookCephv1.CephFilesystemSubVolumeGroupSpec{
			FilesystemName: cephFilesystem.Name,
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

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.getNamespacedName(), "rbd", "provisioner")
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

		addStorageRelatedAnnotations(r.cephClientNode, r.getNamespacedName(), "rbd", "node")
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

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.getNamespacedName(), "cephfs", "provisioner")
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

		addStorageRelatedAnnotations(r.cephClientNode, r.getNamespacedName(), "cephfs", "node")
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

func addStorageRelatedAnnotations(obj client.Object, storageClassClaimNamespacedName, storageClaim, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[v1alpha1.StorageClassClaimAnnotation] = storageClassClaimNamespacedName
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

func (r *StorageClassClaimReconciler) getNamespacedName() string {
	return fmt.Sprintf("%s/%s", r.storageClassClaim.Namespace, r.storageClassClaim.Name)
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

// addLabel add a label to a resource metadata
func addLabel(obj metav1.Object, key string, value string) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = map[string]string{}
		obj.SetLabels(labels)
	}
	labels[key] = value
}

// addAnnotation add a annotation to a resource metadata
func addAnnotation(obj metav1.Object, key string, value string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}
	annotations[key] = value
}

// generateUUID generates a random UUID string and return first 8 characters.
func generateUUID() string {
	newUUID := uuid.New().String()
	return newUUID[:8]
}
