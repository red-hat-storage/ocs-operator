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

	"github.com/go-logr/logr"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storageConsumerFinalizer      = "storagesconsumer.ocs.openshift.io"
	StorageConsumerAnnotation     = "ocs.openshift.io.storageconsumer"
	StorageClaimAnnotation        = "ocs.openshift.io.storageclaim"
	StorageCephUserTypeAnnotation = "ocs.openshift.io.cephusertype"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                          context.Context
	storageConsumer              *ocsv1alpha1.StorageConsumer
	cephBlockPool                *rookCephv1.CephBlockPool
	cephFilesystemSubVolumeGroup *rookCephv1.CephFilesystemSubVolumeGroup
	cephClientRBDProvisioner     *rookCephv1.CephClient
	cephClientRBDNode            *rookCephv1.CephClient
	cephClientCephFSProvisioner  *rookCephv1.CephClient
	cephClientCephFSNode         *rookCephv1.CephClient
	cephClientHealthChecker      *rookCephv1.CephClient
	cephResourcesByName          map[string]*ocsv1alpha1.CephResourcesSpec
	namespace                    string
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephfilesystemsubvolumegroups,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch

// Reconcile reads that state of the cluster for a StorageConsumer object and makes changes based on the state read
// and what is in the StorageConsumer.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *StorageConsumerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	r.Log.Info("Reconciling StorageConsumer.", "StorageConsumer", klog.KRef(request.Namespace, request.Name))

	// Initalize the reconciler properties from the request
	r.initReconciler(request)

	if err := r.get(r.storageConsumer); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("No StorageConsumer resource.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to retrieve StorageConsumer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
		return reconcile.Result{}, err
	}

	// Reconcile changes to the cluster
	result, reconcileError := r.reconcilePhases()

	// Apply status changes to the StorageConsumer
	statusError := r.Client.Status().Update(r.ctx, r.storageConsumer)
	if statusError != nil {
		r.Log.Info("Could not update StorageConsumer status.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	}

	return result, nil

}

func (r *StorageConsumerReconciler) initReconciler(request reconcile.Request) {
	r.ctx = context.Background()
	r.namespace = request.Namespace
	r.cephResourcesByName = map[string]*ocsv1alpha1.CephResourcesSpec{}

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{}
	r.storageConsumer.Name = request.Name
	r.storageConsumer.Namespace = r.namespace

	r.cephBlockPool = &rookCephv1.CephBlockPool{}
	r.cephBlockPool.Name = fmt.Sprintf("%s-%s", "cephblockpool", r.storageConsumer.Name)
	r.cephBlockPool.Namespace = r.namespace

	r.cephFilesystemSubVolumeGroup = &rookCephv1.CephFilesystemSubVolumeGroup{}
	r.cephFilesystemSubVolumeGroup.Name = fmt.Sprintf("%s-%s", "cephfilesystemsubvolumegroup", r.storageConsumer.Name)
	r.cephFilesystemSubVolumeGroup.Namespace = r.namespace

	r.cephClientRBDProvisioner = &rookCephv1.CephClient{}
	r.cephClientRBDProvisioner.Name = generateHashForCephClient(r.storageConsumer.Name, "rbd", "provisioner")
	r.cephClientRBDProvisioner.Namespace = r.namespace

	r.cephClientRBDNode = &rookCephv1.CephClient{}
	r.cephClientRBDNode.Name = generateHashForCephClient(r.storageConsumer.Name, "rbd", "node")
	r.cephClientRBDNode.Namespace = r.namespace

	r.cephClientCephFSProvisioner = &rookCephv1.CephClient{}
	r.cephClientCephFSProvisioner.Name = generateHashForCephClient(r.storageConsumer.Name, "cephfs", "provisioner")
	r.cephClientCephFSProvisioner.Namespace = r.namespace

	r.cephClientCephFSNode = &rookCephv1.CephClient{}
	r.cephClientCephFSNode.Name = generateHashForCephClient(r.storageConsumer.Name, "cephfs", "node")
	r.cephClientCephFSNode.Namespace = r.namespace

	r.cephClientHealthChecker = &rookCephv1.CephClient{}
	r.cephClientHealthChecker.Name = generateHashForCephClient(r.storageConsumer.Name, "global", "healthChecker")
	r.cephClientHealthChecker.Namespace = r.namespace
}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {

	r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring

	for _, cephResourceSpec := range r.storageConsumer.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		if !contains(r.storageConsumer.GetFinalizers(), storageConsumerFinalizer) {
			r.Log.Info("Finalizer not found for StorageConsumer. Adding finalizer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
			r.storageConsumer.ObjectMeta.Finalizers = append(r.storageConsumer.ObjectMeta.Finalizers, storageConsumerFinalizer)
			if err := r.Client.Update(r.ctx, r.storageConsumer); err != nil {
				r.Log.Error(err, "Failed to update StorageConsumer with finalizer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
				return reconcile.Result{}, err
			}
		}

		if err := r.reconcileCephClientRBDProvisioner(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephClientRBDNode(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephBlockPool(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephClientCephFSProvisioner(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephClientCephFSNode(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephFilesystemSubVolumeGroup(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephClientHealthChecker(); err != nil {
			return reconcile.Result{}, err
		}

		cephResourcesReady := true
		for _, cephResource := range r.storageConsumer.Status.CephResources {
			if cephResource.Phase != "Ready" {
				cephResourcesReady = false
				break
			}
		}

		if cephResourcesReady {
			r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateReady
		}

	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting

		if r.verifyCephResourcesDoNotExist() {
			r.Log.Info("Removing finalizer from StorageConsumer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
			r.storageConsumer.ObjectMeta.Finalizers = remove(r.storageConsumer.ObjectMeta.Finalizers, storageConsumerFinalizer)
			if err := r.Client.Update(r.ctx, r.storageConsumer); err != nil {
				r.Log.Error(err, "Failed to remove finalizer from StorageConsumer", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
				return reconcile.Result{}, err
			}
		} else {
			for _, cephResource := range r.storageConsumer.Status.CephResources {
				switch cephResource.Kind {
				case "CephClient":
					cephClient := &rookCephv1.CephClient{}
					cephClient.Name = cephResource.Name
					cephClient.Namespace = r.namespace
					if err := r.delete(cephClient); err != nil {
						return ctrl.Result{}, fmt.Errorf("unable to delete CephClient : %v", err)
					}
				case "CephBlockPool":
					cephBlockPool := &rookCephv1.CephBlockPool{}
					cephBlockPool.Name = cephResource.Name
					cephBlockPool.Namespace = r.namespace
					if err := r.delete(cephBlockPool); err != nil {
						return ctrl.Result{}, fmt.Errorf("unable to delete CephBlockPool : %v", err)
					}
				case "CephFilesystemSubVolumeGroup":
					cephFilesystemSubVolumeGroup := &rookCephv1.CephFilesystemSubVolumeGroup{}
					cephFilesystemSubVolumeGroup.Name = cephResource.Name
					cephFilesystemSubVolumeGroup.Namespace = r.namespace
					if err := r.delete(cephFilesystemSubVolumeGroup); err != nil {
						return ctrl.Result{}, fmt.Errorf("unable to delete CephFilesystemSubVolumeGroup : %v", err)
					}
				}
			}
		}
	}

	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) reconcileCephBlockPool() error {

	storageClusterList := ocsv1.StorageClusterList{}
	err := r.Client.List(r.ctx, &storageClusterList, client.InNamespace(r.namespace))
	if err != nil {
		return fmt.Errorf("Error fetching StorageClusterList. %+v", err)
	}

	var failureDomain string
	if len(storageClusterList.Items) != 1 {
		return fmt.Errorf("Cluster has none or more than 1 StorageCluster")
	}

	failureDomain = storageClusterList.Items[0].Status.FailureDomain

	capacity := r.storageConsumer.Spec.Capacity.String()

	_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephBlockPool, func() error {
		if err := r.own(r.cephBlockPool); err != nil {
			return err
		}

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
		r.Log.Error(
			err,
			"Failed to update CephBlockPool.",
			"CephBlockPool",
			klog.KRef(r.cephBlockPool.Namespace, r.cephBlockPool.Name),
		)
		return err
	}

	cephClients := map[string]string{
		"provisioner": r.cephClientRBDProvisioner.Name,
		"node":        r.cephClientRBDNode.Name,
	}
	phase := ""
	if r.cephBlockPool.Status != nil {
		phase = string(r.cephBlockPool.Status.Phase)
	}

	r.setCephResourceStatus(r.cephBlockPool.Name, "CephBlockPool", phase, cephClients)

	r.storageConsumer.Status.GrantedCapacity = r.storageConsumer.Spec.Capacity

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephFilesystemSubVolumeGroup() error {

	cephFilesystemList := rookCephv1.CephFilesystemList{}
	if err := r.Client.List(r.ctx, &cephFilesystemList, client.InNamespace(r.namespace)); err != nil {
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
		r.Log.Error(
			err,
			"Failed to update CephFilesystemSubVolumeGroup.",
			"CephFilesystemSubVolumeGroup",
			klog.KRef(r.cephFilesystemSubVolumeGroup.Namespace, r.cephFilesystemSubVolumeGroup.Name),
		)
		return err
	}

	cephClients := map[string]string{
		"provisioner": r.cephClientCephFSProvisioner.Name,
		"node":        r.cephClientCephFSNode.Name,
	}
	phase := ""
	if r.cephFilesystemSubVolumeGroup.Status != nil {
		phase = string(r.cephFilesystemSubVolumeGroup.Status.Phase)
	}

	r.setCephResourceStatus(r.cephFilesystemSubVolumeGroup.Name, "CephFilesystemSubVolumeGroup", phase, cephClients)

	return nil

}

func (r *StorageConsumerReconciler) reconcileCephClientRBDProvisioner() error {

	desired := &rookCephv1.CephClient{
		Spec: rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s", r.cephBlockPool.Name),
			},
		},
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientRBDProvisioner, func() error {
		if err := r.own(r.cephClientRBDProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientRBDProvisioner, r.storageConsumer.Name, "rbd", "provisioner")
		r.cephClientRBDProvisioner.Spec = desired.Spec
		return nil
	})

	if err != nil {
		r.Log.Error(err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientRBDProvisioner.Namespace, r.cephClientRBDProvisioner.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientRBDProvisioner.Status != nil {
		phase = string(r.cephClientRBDProvisioner.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientRBDProvisioner.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientRBDNode() error {

	desired := &rookCephv1.CephClient{
		Spec: rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mon": "profile rbd",
				"mgr": "allow rw",
				"osd": fmt.Sprintf("profile rbd pool=%s", r.cephBlockPool.Name),
			},
		},
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientRBDNode, func() error {
		if err := r.own(r.cephClientRBDNode); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientRBDNode, r.storageConsumer.Name, "rbd", "node")
		r.cephClientRBDNode.Spec = desired.Spec
		return nil
	})

	if err != nil {
		r.Log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientRBDNode.Namespace, r.cephClientRBDNode.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientRBDNode.Status != nil {
		phase = string(r.cephClientRBDNode.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientRBDNode.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientCephFSProvisioner() error {

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientCephFSProvisioner, func() error {
		if err := r.own(r.cephClientCephFSProvisioner); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientCephFSProvisioner, r.storageConsumer.Name, "cephfs", "provisioner")
		r.cephClientCephFSProvisioner.Spec = rookCephv1.ClientSpec{
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
		r.Log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientCephFSProvisioner.Namespace, r.cephClientCephFSProvisioner.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientCephFSProvisioner.Status != nil {
		phase = string(r.cephClientCephFSProvisioner.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientCephFSProvisioner.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientCephFSNode() error {

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientCephFSNode, func() error {
		if err := r.own(r.cephClientCephFSNode); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientCephFSNode, r.storageConsumer.Name, "cephfs", "node")
		r.cephClientCephFSNode.Spec = rookCephv1.ClientSpec{
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
		r.Log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientCephFSNode.Namespace, r.cephClientCephFSNode.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientCephFSNode.Status != nil {
		phase = string(r.cephClientCephFSNode.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientCephFSNode.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageConsumerReconciler) reconcileCephClientHealthChecker() error {

	desired := &rookCephv1.CephClient{
		Spec: rookCephv1.ClientSpec{
			Caps: map[string]string{
				"mgr": "allow command config",
				"mon": "allow r, allow command quorum_status, allow command version",
			},
		},
	}

	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.cephClientHealthChecker, func() error {
		if err := r.own(r.cephClientHealthChecker); err != nil {
			return err
		}

		addStorageRelatedAnnotations(r.cephClientHealthChecker, r.storageConsumer.Name, "global", "healthchecker")
		r.cephClientHealthChecker.Spec = desired.Spec
		return nil
	})

	if err != nil {
		r.Log.Error(
			err,
			"Failed to update CephClient.",
			"CephClient",
			klog.KRef(r.cephClientHealthChecker.Namespace, r.cephClientHealthChecker.Name),
		)
		return err
	}

	phase := ""
	if r.cephClientHealthChecker.Status != nil {
		phase = string(r.cephClientHealthChecker.Status.Phase)
	}

	r.setCephResourceStatus(r.cephClientHealthChecker.Name, "CephClient", phase, nil)

	return nil
}

func (r *StorageConsumerReconciler) verifyCephResourcesDoNotExist() bool {
	for _, cephResource := range r.storageConsumer.Status.CephResources {
		switch cephResource.Kind {
		case "CephClient":
			cephClient := &rookCephv1.CephClient{}
			cephClient.Name = cephResource.Name
			cephClient.Namespace = r.namespace
			if err := r.get(cephClient); err == nil || !errors.IsNotFound(err) {
				return false
			}
		case "CephBlockPool":
			cephBlockPool := &rookCephv1.CephBlockPool{}
			cephBlockPool.Name = cephResource.Name
			cephBlockPool.Namespace = r.namespace
			if err := r.get(cephBlockPool); err == nil || !errors.IsNotFound(err) {
				return false
			}
		case "CephFilesystemSubVolumeGroup":
			cephFilesystemSubVolumeGroup := &rookCephv1.CephFilesystemSubVolumeGroup{}
			cephFilesystemSubVolumeGroup.Name = cephResource.Name
			cephFilesystemSubVolumeGroup.Namespace = r.namespace
			if err := r.get(cephFilesystemSubVolumeGroup); err == nil || !errors.IsNotFound(err) {
				return false
			}
		}
	}
	return true
}

func (r *StorageConsumerReconciler) setCephResourceStatus(name string, kind string, phase string, cephClients map[string]string) {

	cephResourceSpec := r.cephResourcesByName[name]

	if cephResourceSpec == nil {
		cephResourceSpec = &ocsv1alpha1.CephResourcesSpec{
			Name:        name,
			Kind:        kind,
			CephClients: cephClients,
		}
		r.storageConsumer.Status.CephResources = append(r.storageConsumer.Status.CephResources, cephResourceSpec)
		r.cephResourcesByName[name] = cephResourceSpec
	}

	cephResourceSpec.Phase = phase
}

func (r *StorageConsumerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageConsumerReconciler) delete(obj client.Object) error {
	if err := r.Client.Delete(r.ctx, obj); err != nil && !errors.IsNotFound(err) {
		return err
	}
	return nil
}

// Checks whether a string is contained within a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

func (r *StorageConsumerReconciler) own(resource metav1.Object) error {
	// Ensure storageConsumer ownership on a resource
	return ctrl.SetControllerReference(r.storageConsumer, resource, r.Scheme)
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

// SetupWithManager sets up the controller with the Manager.
func (r *StorageConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1alpha1.StorageConsumer{}).
		Owns(&rookCephv1.CephBlockPool{}).
		Owns(&rookCephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&rookCephv1.CephClient{}).
		Complete(r)
}

func generateHashForCephClient(storageConsumerName, claimID, cephUserType string) string {
	var c struct {
		StorageConsumerName string `json:"id"`
		ClaimID             string `json:"claimId"`
		CephUserType        string `json:"cephUserType"`
	}

	c.StorageConsumerName = storageConsumerName
	c.ClaimID = claimID
	c.CephUserType = cephUserType

	cephClient, err := json.Marshal(c)
	if err != nil {
		klog.Errorf("failed to marshal ceph client name for consumer %s. %v", storageConsumerName, err)
		panic("failed to marshal")
	}
	name := md5.Sum([]byte(cephClient))
	return hex.EncodeToString(name[:16])
}

func addStorageRelatedAnnotations(obj client.Object, storageConsumerName, storageClaim, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[StorageConsumerAnnotation] = storageConsumerName
	annotations[StorageClaimAnnotation] = storageClaim
	annotations[StorageCephUserTypeAnnotation] = cephUserType
}
