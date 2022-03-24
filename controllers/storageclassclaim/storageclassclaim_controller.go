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
	"fmt"

	"github.com/go-logr/logr"
	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/controllers/storagecluster"
	controllers "github.com/red-hat-storage/ocs-operator/controllers/storageconsumer"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"

	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storageClassClaimFinalizer  = "storagesclassclaim.ocs.openshift.io"
	StorageClassClaimAnnotation = "ocs.openshift.io.storagesclassclaim"
)

// StorageClassClaimReconciler reconciles a StorageClassClaim object
// nolint
type StorageClassClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	log    logr.Logger
	ctx    context.Context

	storageConsumer              *ocsv1alpha1.StorageConsumer
	storageCluster               *v1.StorageCluster
	storageClassClaim            *ocsv1alpha1.StorageClassClaim
	cephBlockPool                *rookCephv1.CephBlockPool
	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientProvisioner        *rookCephv1.CephClient
	cephClientNode               *rookCephv1.CephClient
	cephResourcesByName          map[string]*ocsv1alpha1.CephResourcesSpec

	namespace string
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools,verbs=get;list;watch;create;update;delete

func (r *StorageClassClaimReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	r.log = ctrllog.FromContext(ctx, "StorageClassClaim", request)
	r.ctx = ctrllog.IntoContext(ctx, r.log)
	r.namespace = request.Namespace
	r.log.Info("Reconciling StorageClassClaim.")

	// Fetch the StorageClassClaim instance
	r.storageClassClaim = &ocsv1alpha1.StorageClassClaim{}
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

	storageClusterList := &v1.StorageClusterList{}
	if err := r.Client.List(ctx, storageClusterList, client.InNamespace(request.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	if len(storageClusterList.Items) > 1 {
		return reconcile.Result{}, fmt.Errorf("multiple StorageCluster found")
	}
	r.storageCluster = &storageClusterList.Items[0]

	var result reconcile.Result
	var reconcileError error
	if storagecluster.IsOCSConsumerMode(r.storageCluster) {
		result, reconcileError = r.reconcileConsumerPhases(ctx, r.storageCluster)
	} else {
		result, reconcileError = r.reconcileProviderPhases()
	}

	// Apply status changes to the StorageClassClaim
	statusError := r.Client.Status().Update(ctx, r.storageClassClaim)

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
		For(&ocsv1alpha1.StorageClassClaim{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&rookCephv1.CephBlockPool{}).
		Owns(&rookCephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&rookCephv1.CephClient{}).
		Complete(r)
}

func (r *StorageClassClaimReconciler) reconcileConsumerPhases(ctx context.Context, storagecluster *v1.StorageCluster) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Running StorageClassClaim controller in Consumer Mode")
	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) reconcileProviderPhases() (reconcile.Result, error) {
	r.log.Info("Running StorageClassClaim controller in Converged/Provider Mode")

	r.storageClassClaim.Status.Phase = string(v1alpha1.StorageClassClaimStateInitializing)

	// reading storageConsumer Name from storageClassClaim ownerReferences
	ownerRefs := r.storageClassClaim.GetOwnerReferences()
	for i := range ownerRefs {
		if ownerRefs[i].Kind == r.storageConsumer.Kind {
			r.storageConsumer.Name = ownerRefs[i].Name
		}
	}
	if r.storageConsumer.Name == "" {
		return reconcile.Result{}, fmt.Errorf("No storage consumer owner ref on the storage class claim")
	}

	if err := r.get(r.storageConsumer); err != nil {
		return reconcile.Result{}, err
	}

	if r.storageClassClaim.Spec.Type == "blockpool" {
		r.cephBlockPool = &rookCephv1.CephBlockPool{}
		r.cephBlockPool.Name = fmt.Sprintf("cephblockpool-%s", r.storageConsumer.Name)
		r.cephBlockPool.Namespace = r.namespace
		r.cephBlockPool.Labels[controllers.StorageConsumerNameLabel] = r.storageConsumer.Name

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

	for _, cephResourceSpec := range r.storageClassClaim.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	r.storageClassClaim.Status.Phase = string(v1alpha1.StorageClassClaimStateCreating)

	if r.storageClassClaim.GetDeletionTimestamp().IsZero() {
		if !controllers.Contains(r.storageClassClaim.GetFinalizers(), storageClassClaimFinalizer) {
			r.log.Info("Finalizer not found for StorageClassClaim. Adding finalizer.", "StorageClassClaim", klog.KRef(r.storageClassClaim.Namespace, r.storageClassClaim.Name))
			r.storageClassClaim.SetFinalizers(append(r.storageClassClaim.GetFinalizers(), storageClassClaimFinalizer))
			if err := r.update(r.storageClassClaim); err != nil {
				r.log.Error(err, "Failed to update StorageClassClaim with finalizer.", "StorageClassClaim", klog.KRef(r.storageClassClaim.Namespace, r.storageClassClaim.Name))
				return reconcile.Result{}, err
			}
		}
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
			r.storageClassClaim.Status.Phase = string(v1alpha1.StorageClassClaimStateReady)
		}

	}

	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) reconcileCephBlockPool() error {

	failureDomain := r.storageCluster.Status.FailureDomain

	capacity := r.storageConsumer.Spec.Capacity.String()

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephBlockPool, func() error {
		if err := r.own(r.cephBlockPool); err != nil {
			return err
		}

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
	if err := r.list(&cephFilesystemList, []client.ListOption{client.InNamespace(r.namespace)}); err != nil {
		return fmt.Errorf("Error fetching CephFilesystemList. %+v", err)
	}

	var cephFileSystemName string
	availableCephFileSystems := len(cephFilesystemList.Items)
	if availableCephFileSystems == 0 {
		return fmt.Errorf("No CephFileSystem found in the cluster")
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
		cephResourceSpec = &ocsv1alpha1.CephResourcesSpec{
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

func (r *StorageClassClaimReconciler) own(resource metav1.Object) error {
	// Ensure storageClassClaim ownership on a resource
	return ctrl.SetControllerReference(r.storageClassClaim, resource, r.Scheme)
}

func (r *StorageClassClaimReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageClassClaimReconciler) update(obj client.Object) error {
	return r.Client.Update(r.ctx, obj)
}

func (r *StorageClassClaimReconciler) list(obj client.ObjectList, listOpt []client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOpt...)
}
