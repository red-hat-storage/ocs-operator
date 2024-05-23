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

package storagerequest

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/go-logr/logr"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v7/apis/volumesnapshot/v1"
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

const (
	storageRequestFinalizer    = "ocs.openshift.io.storagerequest"
	forceDeletionAnnotationKey = "rook.io/force-deletion"
)

// StorageRequestReconciler reconciles a StorageRequest object
// nolint:revive
type StorageRequestReconciler struct {
	client.Client
	cache.Cache
	Scheme            *runtime.Scheme
	OperatorNamespace string

	log                          logr.Logger
	ctx                          context.Context
	storageConsumer              *v1alpha1.StorageConsumer
	storageCluster               *v1.StorageCluster
	StorageRequest               *v1alpha1.StorageRequest
	cephRadosNamespace           *rookCephv1.CephBlockPoolRadosNamespace
	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientProvisioner        *rookCephv1.CephClient
	cephClientNode               *rookCephv1.CephClient
	cephResourcesByName          map[string]*v1alpha1.CephResourcesSpec
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpoolradosnamespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;watch;list
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;list;watch;create;delete
// +kubebuilder:rbac:groups=core,resources=persistentvolumes,verbs=get;list;watch

func (r *StorageRequestReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	if ok := r.Cache.WaitForCacheSync(ctx); !ok {
		return reconcile.Result{}, fmt.Errorf("cache sync failed")
	}

	r.log = ctrllog.FromContext(ctx, "StorageRequest", request)
	r.ctx = ctrllog.IntoContext(ctx, r.log)
	r.log.Info("Reconciling StorageRequest.")

	// Fetch the StorageRequest instance
	r.StorageRequest = &v1alpha1.StorageRequest{}
	r.StorageRequest.Name = request.Name
	r.StorageRequest.Namespace = request.Namespace

	if err := r.get(r.StorageRequest); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageRequest resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageRequest.")
		return reconcile.Result{}, err
	}

	r.StorageRequest.Status.Phase = v1alpha1.StorageRequestInitializing

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

	// Apply status changes to the StorageRequest
	statusError := r.Client.Status().Update(r.ctx, r.StorageRequest)
	if statusError != nil {
		r.log.Info("Failed to update StorageRequest status.")
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

func (r *StorageRequestReconciler) SetupWithManager(mgr ctrl.Manager) error {

	if err := mgr.GetCache().IndexField(
		context.TODO(),
		&rookCephv1.CephBlockPoolRadosNamespace{},
		util.OwnerUIDIndexName,
		util.OwnersIndexFieldFunc,
	); err != nil {
		return fmt.Errorf("unable to set up FieldIndexer on CephBlockPoolRadosNamespaces for owner reference UIDs: %v", err)
	}

	if err := mgr.GetCache().IndexField(
		context.TODO(),
		&rookCephv1.CephFilesystemSubVolumeGroup{},
		util.OwnerUIDIndexName,
		util.OwnersIndexFieldFunc,
	); err != nil {
		return fmt.Errorf("unable to set up FieldIndexer on CephFilesystemSubVolumeGroups for owner reference UIDs: %v", err)
	}

	enqueueStorageConsumerRequest := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			annotations := obj.GetAnnotations()
			if annotation, found := annotations[v1alpha1.StorageRequestAnnotation]; found {
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
	enqueueForOwner := handler.EnqueueRequestForOwner(mgr.GetScheme(), mgr.GetRESTMapper(), &v1alpha1.StorageRequest{})

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.StorageRequest{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&rookCephv1.CephBlockPoolRadosNamespace{}).
		Watches(&rookCephv1.CephFilesystemSubVolumeGroup{}, enqueueForOwner).
		Watches(&rookCephv1.CephClient{}, enqueueForOwner).
		Watches(&storagev1.StorageClass{}, enqueueStorageConsumerRequest).
		Watches(&snapapi.VolumeSnapshotClass{}, enqueueStorageConsumerRequest).
		Complete(r)
}

func (r *StorageRequestReconciler) initPhase() error {
	gvk, err := apiutil.GVKForObject(&v1alpha1.StorageConsumer{}, r.Client.Scheme())
	if err != nil {
		return fmt.Errorf("failed to get gvk for consumer  %w", err)
	}
	// reading storageConsumer Name from StorageRequest ownerReferences
	ownerRefs := r.StorageRequest.GetOwnerReferences()
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

	// check request status already contains the name of the resource. if not, add it.
	if r.StorageRequest.Spec.Type == "block" {
		// initialize in-memory structs
		r.cephRadosNamespace = &rookCephv1.CephBlockPoolRadosNamespace{}
		r.cephRadosNamespace.Namespace = r.OperatorNamespace

		// check if a CephBlockPoolRadosNamespace resource exists for the desired storageconsumer and storageprofile.
		cephRadosNamespaceList := &rookCephv1.CephBlockPoolRadosNamespaceList{}
		err := r.list(
			cephRadosNamespaceList,
			client.InNamespace(r.OperatorNamespace),
			client.MatchingFields{util.OwnerUIDIndexName: string(r.StorageRequest.UID)})
		if err != nil {
			return err
		}

		// if we found no CephBlockPoolRadosNamespaces, generate a new name
		// if we found only one CephBlockPoolRadosNamespace with our query, we're good
		// if we found more than one CephBlockPoolRadosNamespace, we can't determine which one to select, so error out
		rnsItemsLen := len(cephRadosNamespaceList.Items)
		if rnsItemsLen == 0 {
			md5Sum := md5.Sum([]byte(r.StorageRequest.Name))
			rnsName := fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(md5Sum[:16]))
			r.log.V(1).Info("no valid CephBlockPoolRadosNamespace found, creating new one", "CephBlockPoolRadosNamespace", rnsName)
			r.cephRadosNamespace.Name = rnsName
		} else if rnsItemsLen == 1 {
			r.cephRadosNamespace = &cephRadosNamespaceList.Items[0]
			r.log.V(1).Info("valid CephBlockPoolRadosNamespace found", "CephBlockPoolRadosNamespace", r.cephRadosNamespace.Name)
		} else {
			return fmt.Errorf("invalid number of CephBlockPoolRadosNamespaces for storage consumer %q: found %d, expecting 0 or 1", r.storageConsumer.Name, rnsItemsLen)
		}
	} else if r.StorageRequest.Spec.Type == "sharedfile" {
		r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
		r.cephFilesystemSubVolumeGroup.Namespace = r.OperatorNamespace

		cephFilesystemSubVolumeGroupList := &rookCephv1.CephFilesystemSubVolumeGroupList{}
		err := r.list(
			cephFilesystemSubVolumeGroupList,
			client.InNamespace(r.OperatorNamespace),
			client.MatchingFields{util.OwnerUIDIndexName: string(r.StorageRequest.UID)})
		if err != nil {
			return err
		}

		svgItemsLen := len(cephFilesystemSubVolumeGroupList.Items)
		if svgItemsLen == 0 {
			md5Sum := md5.Sum([]byte(r.StorageRequest.Name))
			r.cephFilesystemSubVolumeGroup.Name = fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(md5Sum[:16]))
		} else if svgItemsLen == 1 {
			r.cephFilesystemSubVolumeGroup.Name = cephFilesystemSubVolumeGroupList.Items[0].GetName()
			r.log.V(1).Info(fmt.Sprintf("CephFilesystemSubVolumeGroup found: %s", r.cephFilesystemSubVolumeGroup.Name))
		} else {
			return fmt.Errorf(
				"invalid number of CephFilesystemSubVolumeGroups owned by StorageRequest %q: expecting 0-1, found %d", r.StorageRequest.Name, svgItemsLen)
		}
	}

	r.cephClientProvisioner = &rookCephv1.CephClient{}
	r.cephClientProvisioner.Name = controllers.GenerateHashForCephClient(r.StorageRequest.Name, "provisioner")
	r.cephClientProvisioner.Namespace = r.OperatorNamespace

	r.cephClientNode = &rookCephv1.CephClient{}
	r.cephClientNode.Name = controllers.GenerateHashForCephClient(r.StorageRequest.Name, "node")
	r.cephClientNode.Namespace = r.OperatorNamespace

	r.cephResourcesByName = map[string]*v1alpha1.CephResourcesSpec{}

	for _, cephResourceSpec := range r.StorageRequest.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	return nil
}

func (r *StorageRequestReconciler) reconcilePhases() (reconcile.Result, error) {
	r.log.Info("Running StorageRequest controller in Converged/Provider Mode")

	r.StorageRequest.Status.Phase = v1alpha1.StorageRequestInitializing

	if err := r.initPhase(); err != nil {
		return reconcile.Result{}, err
	}

	r.StorageRequest.Status.Phase = v1alpha1.StorageRequestCreating

	if r.StorageRequest.GetDeletionTimestamp().IsZero() {
		if controllerutil.AddFinalizer(r.StorageRequest, storageRequestFinalizer) {
			if err := r.update(r.StorageRequest); err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to add finalizer: %v", err)
			}
		}

		// we are loading the storageconsumer only to confirm it's presence
		if err := r.get(r.storageConsumer); err != nil {
			return reconcile.Result{}, err
		}

		if r.StorageRequest.Spec.Type == "block" {

			if err := r.reconcileCephClientRBDProvisioner(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileCephClientRBDNode(); err != nil {
				return reconcile.Result{}, err
			}

			if err := r.reconcileRadosNamespace(); err != nil {
				return reconcile.Result{}, err
			}

		} else if r.StorageRequest.Spec.Type == "sharedfile" {
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
		for _, cephResource := range r.StorageRequest.Status.CephResources {
			if cephResource.Phase != "Ready" {
				cephResourcesReady = false
				break
			}
		}

		if cephResourcesReady {
			r.StorageRequest.Status.Phase = v1alpha1.StorageRequestReady
		}

	} else {
		r.StorageRequest.Status.Phase = v1alpha1.StorageRequestDeleting
		if err := r.deletionPhase(); err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageRequestReconciler) reconcileRadosNamespace() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephRadosNamespace, func() error {
		if err := r.own(r.cephRadosNamespace); err != nil {
			return err
		}

		addLabel(r.cephRadosNamespace, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)

		// For RADOS namespaces, the "profile" is equivalent to the
		// name of the desired block pool for the namespace.
		blockPoolName := r.StorageRequest.Spec.StorageProfile
		if blockPoolName == "" {
			blockPoolName = fmt.Sprintf("%s-cephblockpool", r.storageCluster.Name)
		}
		r.cephRadosNamespace.Spec = rookCephv1.CephBlockPoolRadosNamespaceSpec{
			BlockPoolName: blockPoolName,
		}
		return nil
	})

	if err != nil {
		r.log.Error(
			err,
			"Failed to update CephBlockPoolRadosNamespace.",
			"CephBlockPoolRadosNamespace",
			klog.KRef(r.cephRadosNamespace.Namespace, r.cephRadosNamespace.Name),
		)
		return err
	}

	cephClients := map[string]string{
		"provisioner": r.cephClientProvisioner.Name,
		"node":        r.cephClientNode.Name,
	}
	phase := ""
	if r.cephRadosNamespace.Status != nil {
		phase = string(r.cephRadosNamespace.Status.Phase)
	}

	r.setCephResourceStatus(r.cephRadosNamespace.Name, "CephBlockPoolRadosNamespace", phase, cephClients)

	return nil
}

func (r *StorageRequestReconciler) reconcileCephFilesystemSubVolumeGroup() error {

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

		// For subvolume groups, the "profile" is equivalent to the
		// name of the desired data pool ("" by default).
		var dataPool *rookCephv1.NamedPoolSpec
		for i := range cephFilesystem.Spec.DataPools {
			if cephFilesystem.Spec.DataPools[i].Name == r.StorageRequest.Spec.StorageProfile {
				dataPool = &cephFilesystem.Spec.DataPools[i]
				break
			}
		}
		if dataPool == nil {
			return fmt.Errorf("no CephFileSystem found in the cluster for storage profile %s", r.StorageRequest.Spec.StorageProfile)
		}

		addLabel(r.cephFilesystemSubVolumeGroup, controllers.StorageConsumerNameLabel, r.storageConsumer.Name)
		// This label is required to set the dataPool on the CephFS
		// storageclass so that each PVC created from CephFS storageclass can
		// use correct dataPool backed by deviceclass.
		var dataPoolValue string
		if dataPool.Name != "" {
			dataPoolValue = fmt.Sprintf("%s-%s", cephFilesystem.Name, dataPool.Name)
		} else {
			// https: //github.com/rook/rook/blob/b3b6775cf9b3ffdd88cd5a3f342ac4883a6a42ac/pkg/operator/ceph/file/filesystem.go#L296
			// the name is auto generated by rook as <cephfsname>-data<%d> where `%d` is the index which is `0` for default pool
			dataPoolValue = fmt.Sprintf("%s-data0", cephFilesystem.Name)
		}
		addLabel(r.cephFilesystemSubVolumeGroup, v1alpha1.CephFileSystemDataPoolLabel, dataPoolValue)

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

func (r *StorageRequestReconciler) reconcileCephClientRBDProvisioner() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientProvisioner, func() error {
		if err := r.own(r.cephClientProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.getNamespacedName(), "rbd", "provisioner")
		r.cephClientProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd, allow command 'osd blocklist'",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s namespace=%s", r.cephRadosNamespace.Spec.BlockPoolName, r.cephRadosNamespace.Name),
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

func (r *StorageRequestReconciler) reconcileCephClientRBDNode() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientNode, func() error {
		if err := r.own(r.cephClientNode); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientNode, r.getNamespacedName(), "rbd", "node")
		r.cephClientNode.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s namespace=%s", r.cephRadosNamespace.Spec.BlockPoolName, r.cephRadosNamespace.Name),
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

func (r *StorageRequestReconciler) reconcileCephClientCephFSProvisioner() error {

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientProvisioner, func() error {
		if err := r.own(r.cephClientProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientProvisioner, r.getNamespacedName(), "cephfs", "provisioner")
		r.cephClientProvisioner.Spec = rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "allow r, allow command 'osd blocklist'",
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

func (r *StorageRequestReconciler) reconcileCephClientCephFSNode() error {

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

func (r *StorageRequestReconciler) setCephResourceStatus(name string, kind string, phase string, cephClients map[string]string) {

	cephResourceSpec := r.cephResourcesByName[name]

	if cephResourceSpec == nil {
		cephResourceSpec = &v1alpha1.CephResourcesSpec{
			Name:        name,
			Kind:        kind,
			CephClients: cephClients,
		}
		r.StorageRequest.Status.CephResources = append(r.StorageRequest.Status.CephResources, cephResourceSpec)
		r.cephResourcesByName[name] = cephResourceSpec
	}

	cephResourceSpec.Phase = phase
}

func (r *StorageRequestReconciler) deletionPhase() error {
	if r.StorageRequest.Spec.Type == "block" {
		if err := r.get(r.cephRadosNamespace); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get CephRadosNamespace: %v", err)
		} else if err == nil && util.AddAnnotation(r.cephRadosNamespace, forceDeletionAnnotationKey, "true") {
			if err := r.update(r.cephRadosNamespace); err != nil {
				return fmt.Errorf("failed to annotate CephRadosNamespace: %v", err)
			}
		}
	} else if r.StorageRequest.Spec.Type == "sharedfile" {
		if err := r.get(r.cephFilesystemSubVolumeGroup); err != nil && !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get CephFileSystemSubVolumeGroup: %v", err)
		} else if err == nil && util.AddAnnotation(r.cephFilesystemSubVolumeGroup, forceDeletionAnnotationKey, "true") {
			if err := r.update(r.cephFilesystemSubVolumeGroup); err != nil {
				return fmt.Errorf("failed to annotate CephFileSystemSubVolumeGroup: %v", err)
			}
		}
	}

	if controllerutil.RemoveFinalizer(r.StorageRequest, storageRequestFinalizer) {
		if err := r.update(r.StorageRequest); err != nil {
			return fmt.Errorf("failed to remove finalizer: %v", err)
		}
	}
	return nil
}

func addStorageRelatedAnnotations(obj client.Object, storageRequestNamespacedName, storageRequest, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[v1alpha1.StorageRequestAnnotation] = storageRequestNamespacedName
	annotations[controllers.StorageRequestAnnotation] = storageRequest
	annotations[controllers.StorageCephUserTypeAnnotation] = cephUserType
}

func (r *StorageRequestReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageRequestReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *StorageRequestReconciler) update(obj client.Object, opts ...client.UpdateOption) error {
	return r.Update(r.ctx, obj, opts...)
}

func (r *StorageRequestReconciler) own(resource metav1.Object) error {
	// Ensure StorageRequest ownership on a resource
	return controllerutil.SetControllerReference(r.StorageRequest, resource, r.Scheme)
}

func (r *StorageRequestReconciler) getNamespacedName() string {
	return fmt.Sprintf("%s/%s", r.StorageRequest.Namespace, r.StorageRequest.Name)
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
