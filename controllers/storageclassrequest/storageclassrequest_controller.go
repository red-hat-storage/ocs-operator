/*
Copyright 2023 Red Hat OpenShift Container Storage.
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

package storageclassrequest

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	"github.com/google/uuid"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v6/apis/volumesnapshot/v1"
	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	controllers "github.com/red-hat-storage/ocs-operator/v4/controllers/storageconsumer"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
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
)

// StorageClassRequestReconciler reconciles a StorageClassRequest object
// nolint:revive
type StorageClassRequestReconciler struct {
	client.Client
	cache.Cache
	Scheme            *runtime.Scheme
	OperatorNamespace string

	log                          logr.Logger
	ctx                          context.Context
	storageConsumer              *v1alpha1.StorageConsumer
	storageCluster               *v1.StorageCluster
	StorageClassRequest          *v1alpha1.StorageClassRequest
	cephBlockPool                *rookCephv1.CephBlockPool
	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientProvisioner        *rookCephv1.CephClient
	cephClientNode               *rookCephv1.CephClient
	cephResourcesByName          map[string]*v1alpha1.CephResourcesSpec
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassrequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassrequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch

func (r *StorageClassRequestReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if ok := r.Cache.WaitForCacheSync(ctx); !ok {
		return reconcile.Result{}, fmt.Errorf("cache sync failed")
	}

	r.log = ctrllog.FromContext(ctx, "StorageClassRequest", request)
	r.ctx = ctrllog.IntoContext(ctx, r.log)
	r.log.Info("Reconciling StorageClassRequest.")

	// Fetch the StorageClassRequest instance
	r.StorageClassRequest = &v1alpha1.StorageClassRequest{}
	r.StorageClassRequest.Name = request.Name
	r.StorageClassRequest.Namespace = request.Namespace

	if err := r.get(r.StorageClassRequest); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageClassRequest resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageClassRequest.")
		return reconcile.Result{}, err
	}

	r.StorageClassRequest.Status.Phase = v1alpha1.StorageClassRequestInitializing

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

	result, reconcileError = r.reconcilePhases()

	// Apply status changes to the StorageClassRequest
	statusError := r.Client.Status().Update(r.ctx, r.StorageClassRequest)
	if statusError != nil {
		r.log.Info("Failed to update StorageClassRequest status.")
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

func (r *StorageClassRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueStorageConsumerRequest := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			annotations := obj.GetAnnotations()
			if annotation, found := annotations[v1alpha1.StorageClassRequestAnnotation]; found {
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
	enqueueForOwner := handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &v1alpha1.StorageClassRequest{})
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.StorageClassRequest{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Watches(&rookCephv1.CephBlockPool{}, enqueueForOwner).
		Watches(&rookCephv1.CephFilesystemSubVolumeGroup{}, enqueueForOwner).
		Watches(&rookCephv1.CephClient{}, enqueueForOwner).
		Watches(&storagev1.StorageClass{}, enqueueStorageConsumerRequest).
		Watches(&snapapi.VolumeSnapshotClass{}, enqueueStorageConsumerRequest).
		Complete(r)
}

func (r *StorageClassRequestReconciler) initPhase(storageProfile *v1.StorageProfile) error {
	gvk, err := apiutil.GVKForObject(&v1alpha1.StorageConsumer{}, r.Client.Scheme())
	if err != nil {
		return fmt.Errorf("failed to get gvk for consumer  %w", err)
	}
	// reading storageConsumer Name from StorageClassRequest ownerReferences
	ownerRefs := r.StorageClassRequest.GetOwnerReferences()
	for i := range ownerRefs {
		if ownerRefs[i].Kind == gvk.Kind {
			r.storageConsumer = &v1alpha1.StorageConsumer{}
			r.storageConsumer.Name = ownerRefs[i].Name
			r.storageConsumer.Namespace = r.OperatorNamespace
			break
		}
	}
	if r.storageConsumer == nil {
		return fmt.Errorf("no storage consumer owner ref on the storage class request")
	}

	if err := r.get(r.storageConsumer); err != nil {
		return err
	}

	profileName := r.StorageClassRequest.Spec.StorageProfile
	if profileName == "" {
		profileName = r.storageCluster.Spec.DefaultStorageProfile
	}

	// Fetch StorageProfile by name in the StorageCluster's namespace
	storageProfile.Name = profileName
	storageProfile.Namespace = r.storageCluster.Namespace

	if err := r.get(storageProfile); err != nil {
		return fmt.Errorf("no storage profile CR found for storage profile %s", profileName)
	}

	// check request status already contains the name of the resource. if not, add it.
	if r.StorageClassRequest.Spec.Type == "blockpool" {
		r.cephBlockPool = &rookCephv1.CephBlockPool{}
		r.cephBlockPool.Namespace = r.OperatorNamespace
		for _, res := range r.StorageClassRequest.Status.CephResources {
			if res.Kind == "CephBlockPool" {
				r.cephBlockPool.Name = res.Name
				break
			}
		}

		// check if a cephblockpool resource exists for the desired storageconsumer and storageprofile.
		if r.cephBlockPool.Name == "" {
			cephBlockPoolList := &rookCephv1.CephBlockPoolList{}
			listOptions := &client.MatchingLabels{
				controllers.StorageConsumerNameLabel: r.storageConsumer.Name,
				controllers.StorageProfileSpecLabel:  storageProfile.GetSpecHash(),
			}
			if err := r.list(cephBlockPoolList, client.InNamespace(r.OperatorNamespace), listOptions); err != nil {
				return err
			}

			// if we found no CephBlockPools, generate a new name
			// if we found only one CephBlockPool with our query, we're good
			// if we found more than one CephBlockPool, we can't determine which one to select, so error out
			cbpItemsLen := len(cephBlockPoolList.Items)
			if cbpItemsLen == 0 {
				cbpNewName := fmt.Sprintf("cephblockpool-%s-%s", r.storageConsumer.Name, generateUUID())
				r.log.V(1).Info("no valid CephBlockPool found, creating new one", "CephBlockPool", cbpNewName)
				r.cephBlockPool.Name = cbpNewName
			} else if cbpItemsLen == 1 {
				r.cephBlockPool.Name = cephBlockPoolList.Items[0].GetName()
				r.log.V(1).Info("valid CephBlockPool found", "CephBlockPool", r.cephBlockPool.Name)
			} else {
				return fmt.Errorf("invalid number of CephBlockPools for storage consumer %q and storage profile %q: found %d, expecting 0 or 1", r.storageConsumer.Name, storageProfile.Name, cbpItemsLen)
			}
		}

	} else if r.StorageClassRequest.Spec.Type == "sharedfilesystem" {
		r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
		r.cephFilesystemSubVolumeGroup.Namespace = r.OperatorNamespace

		cephFilesystemSubVolumeGroupList := &rookCephv1.CephFilesystemSubVolumeGroupList{}
		err := r.Client.List(r.ctx, cephFilesystemSubVolumeGroupList, client.InNamespace(r.OperatorNamespace))
		if err != nil {
			return err
		}
		ownedCephFilesystemSubVolumeGroups := util.Filter(
			cephFilesystemSubVolumeGroupList.Items,
			func(item *rookCephv1.CephFilesystemSubVolumeGroup) bool {
				for i := range item.OwnerReferences {
					if item.OwnerReferences[i].UID == r.StorageClassRequest.UID {
						return true
					}
				}
				return false
			})

		svgItemsLen := len(ownedCephFilesystemSubVolumeGroups)
		if svgItemsLen == 0 {
			md5Sum := md5.Sum([]byte(r.StorageClassRequest.Name))
			r.cephFilesystemSubVolumeGroup.Name = fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(md5Sum[:16]))
		} else if svgItemsLen == 1 {
			r.cephFilesystemSubVolumeGroup.Name = ownedCephFilesystemSubVolumeGroups[0].GetName()
			r.log.V(1).Info(fmt.Sprintf("CephFilesystemSubVolumeGroup found: %s", r.cephFilesystemSubVolumeGroup.Name))
		} else {
			return fmt.Errorf(
				"invalid number of CephFilesystemSubVolumeGroups owned by StorageClassRequest %q: expecting 0-1, found %d", r.StorageClassRequest.Name, svgItemsLen)
		}
	}

	r.cephClientProvisioner = &rookCephv1.CephClient{}
	r.cephClientProvisioner.Name = controllers.GenerateHashForCephClient(r.StorageClassRequest.Name, "provisioner")
	r.cephClientProvisioner.Namespace = r.OperatorNamespace

	r.cephClientNode = &rookCephv1.CephClient{}
	r.cephClientNode.Name = controllers.GenerateHashForCephClient(r.StorageClassRequest.Name, "node")
	r.cephClientNode.Namespace = r.OperatorNamespace

	r.cephResourcesByName = map[string]*v1alpha1.CephResourcesSpec{}

	for _, cephResourceSpec := range r.StorageClassRequest.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	return nil
}

func (r *StorageClassRequestReconciler) reconcilePhases() (reconcile.Result, error) {
	r.log.Info("Running StorageClassRequest controller in Converged/Provider Mode")

	r.StorageClassRequest.Status.Phase = v1alpha1.StorageClassRequestInitializing

	storageProfile := v1.StorageProfile{}

	if err := r.initPhase(&storageProfile); err != nil {
		return reconcile.Result{}, err
	}

	r.StorageClassRequest.Status.Phase = v1alpha1.StorageClassRequestCreating

	if r.StorageClassRequest.GetDeletionTimestamp().IsZero() {
		if r.StorageClassRequest.Spec.Type == "blockpool" {

			if err := r.reconcileCephClientRBDProvisioner(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephClientRBDNode(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephBlockPool(&storageProfile); err != nil {
				return reconcile.Result{}, err
			}

		} else if r.StorageClassRequest.Spec.Type == "sharedfilesystem" {
			if err := r.reconcileCephClientCephFSProvisioner(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephClientCephFSNode(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephFilesystemSubVolumeGroup(&storageProfile); err != nil {
				return reconcile.Result{}, err
			}
		}
		cephResourcesReady := true
		for _, cephResource := range r.StorageClassRequest.Status.CephResources {
			if cephResource.Phase != "Ready" {
				cephResourcesReady = false
				break
			}
		}

		if cephResourcesReady {
			r.StorageClassRequest.Status.Phase = v1alpha1.StorageClassRequestReady
		}

	} else {
		r.StorageClassRequest.Status.Phase = v1alpha1.StorageClassRequestDeleting
	}
	return reconcile.Result{}, nil
}

func (r *StorageClassRequestReconciler) reconcileCephBlockPool(storageProfile *v1.StorageProfile) error {

	failureDomain := r.storageCluster.Status.FailureDomain

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephBlockPool, func() error {
		if err := r.own(r.cephBlockPool); err != nil {
			return err
		}
		deviceClass := storageProfile.Spec.DeviceClass
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
			return fmt.Errorf("could not find device set with device class %q in storagecluster", deviceClass)
		}

		addLabel(r.cephBlockPool, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)
		addLabel(r.cephBlockPool, controllers.StorageProfileSpecLabel, storageProfile.GetSpecHash())

		r.cephBlockPool.Spec = rookCephv1.NamedBlockPoolSpec{
			PoolSpec: rookCephv1.PoolSpec{
				FailureDomain: failureDomain,
				DeviceClass:   deviceClass,
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     3,
					ReplicasPerFailureDomain: 1,
				},
				Parameters: storageProfile.Spec.BlockPoolConfiguration.Parameters,
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

func (r *StorageClassRequestReconciler) reconcileCephFilesystemSubVolumeGroup(storageProfile *v1.StorageProfile) error {

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
		deviceClass := storageProfile.Spec.DeviceClass
		var dataPool *rookCephv1.NamedPoolSpec
		for i := range cephFilesystem.Spec.DataPools {
			if cephFilesystem.Spec.DataPools[i].DeviceClass == deviceClass {
				dataPool = &cephFilesystem.Spec.DataPools[i]
				break
			}
		}
		if dataPool == nil {
			return fmt.Errorf("no CephFileSystem found in the cluster for storage profile %s", r.StorageClassRequest.Spec.StorageProfile)
		}

		addLabel(r.cephFilesystemSubVolumeGroup, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)
		addLabel(r.cephFilesystemSubVolumeGroup, controllers.StorageProfileSpecLabel, storageProfile.GetSpecHash())
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

func (r *StorageClassRequestReconciler) reconcileCephClientRBDProvisioner() error {
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

func (r *StorageClassRequestReconciler) reconcileCephClientRBDNode() error {
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

func (r *StorageClassRequestReconciler) reconcileCephClientCephFSProvisioner() error {

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

func (r *StorageClassRequestReconciler) reconcileCephClientCephFSNode() error {

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

func (r *StorageClassRequestReconciler) setCephResourceStatus(name string, kind string, phase string, cephClients map[string]string) {

	cephResourceSpec := r.cephResourcesByName[name]

	if cephResourceSpec == nil {
		cephResourceSpec = &v1alpha1.CephResourcesSpec{
			Name:        name,
			Kind:        kind,
			CephClients: cephClients,
		}
		r.StorageClassRequest.Status.CephResources = append(r.StorageClassRequest.Status.CephResources, cephResourceSpec)
		r.cephResourcesByName[name] = cephResourceSpec
	}

	cephResourceSpec.Phase = phase
}

func addStorageRelatedAnnotations(obj client.Object, storageClassRequestNamespacedName, storageRequest, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[v1alpha1.StorageClassRequestAnnotation] = storageClassRequestNamespacedName
	annotations[controllers.StorageRequestAnnotation] = storageRequest
	annotations[controllers.StorageCephUserTypeAnnotation] = cephUserType
}

func (r *StorageClassRequestReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageClassRequestReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *StorageClassRequestReconciler) own(resource metav1.Object) error {
	// Ensure StorageClassRequest ownership on a resource
	return controllerutil.SetOwnerReference(r.StorageClassRequest, resource, r.Scheme)
}

func (r *StorageClassRequestReconciler) getNamespacedName() string {
	return fmt.Sprintf("%s/%s", r.StorageClassRequest.Namespace, r.StorageClassRequest.Name)
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

// generateUUID generates a random UUID string and return first 8 characters.
func generateUUID() string {
	newUUID := uuid.New().String()
	return newUUID[:8]
}
