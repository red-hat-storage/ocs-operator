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
	"strings"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	StorageConsumerAnnotation         = "ocs.openshift.io.storageconsumer"
	StorageRequestAnnotation          = "ocs.openshift.io.storagerequest"
	StorageCephUserTypeAnnotation     = "ocs.openshift.io.cephusertype"
	CephClientSecretNameAnnotationKey = "ocs.openshift.io/ceph-secret-name"
	ConsumerUUIDLabel                 = "ocs.openshift.io/storageconsumer-uuid"
	StorageConsumerNameLabel          = "ocs.openshift.io/storageconsumer-name"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                context.Context
	storageConsumer    *ocsv1alpha1.StorageConsumer
	storageCluster     *ocsv1.StorageCluster
	isLocalConsumer    bool
	isUpdatedProvider  bool
	namespace          string
	radosnamespaceName string
	subVolumeGroupName string
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageConsumerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	enqueueStorageConsumerRequest := handler.EnqueueRequestsFromMapFunc(
		func(context context.Context, obj client.Object) []reconcile.Request {
			labels := obj.GetLabels()
			if value, ok := labels[StorageConsumerNameLabel]; ok {
				return []reconcile.Request{{
					NamespacedName: types.NamespacedName{
						Name:      value,
						Namespace: obj.GetNamespace(),
					},
				}}
			}
			return []reconcile.Request{}
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&nbv1.NooBaaAccount{}).
		Owns(&rookCephv1.CephBlockPoolRadosNamespace{}).
		Owns(&rookCephv1.CephFilesystemSubVolumeGroup{}).
		Owns(&rookCephv1.CephClient{}).
		// Watch non-owned resources
		Watches(&rookCephv1.CephBlockPool{}, enqueueStorageConsumerRequest).
		Watches(&rookCephv1.CephFilesystem{}, enqueueStorageConsumerRequest).
		Complete(r)
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaaaccounts,verbs=get;list;watch;create;update;delete

// Reconcile reads that state of the cluster for a StorageConsumer object and makes changes based on the state read
// and what is in the StorageConsumer.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *StorageConsumerReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.ctx = ctx

	r.storageConsumer = &ocsv1alpha1.StorageConsumer{}
	r.storageConsumer.Name = request.Name
	r.storageConsumer.Namespace = r.namespace

	if err := r.get(r.storageConsumer); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("No StorageConsumer resource.")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to retrieve StorageConsumer.")
		return reconcile.Result{}, err
	}

	if !r.storageConsumer.Spec.Enable {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDisabled
		return reconcile.Result{}, nil
	}

	// Reconcile changes to the cluster
	result, reconcileError := r.reconcilePhases()

	// Apply status changes to the StorageConsumer
	statusError := r.Client.Status().Update(r.ctx, r.storageConsumer)
	if statusError != nil {
		r.Log.Info("Could not update StorageConsumer status.")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	}

	return result, nil

}

func (r *StorageConsumerReconciler) initPhase() error {
	storageCluster, err := util.GetStorageClusterInNamespace(r.ctx, r.Client, r.namespace)
	if err != nil {
		return err
	}
	r.storageCluster = storageCluster

	consumerMode, consumerModeOk := r.storageConsumer.GetAnnotations()[defaults.StorageConsumerOldModeAnnotation]
	consumerType, consumerTypeOk := r.storageConsumer.GetAnnotations()[defaults.StorageConsumerTypeAnnotation]

	r.isLocalConsumer = consumerTypeOk && consumerType == defaults.StorageConsumerTypeLocal
	r.isUpdatedProvider = consumerModeOk && consumerMode == defaults.StorageConsumerOldModeProvider

	//local or updated internal
	if r.isLocalConsumer && (consumerMode == defaults.StorageConsumerOldModeInternal || !consumerModeOk) {
		r.radosnamespaceName = "<implicit>"
		r.subVolumeGroupName = "csi"
	} else if r.isUpdatedProvider {
		// old provider mode
		rnsMd5Sum := md5.Sum([]byte(getStorageRequestName(
			string(r.storageConsumer.UID),
			"ocs-storagecluster-ceph-rbd"),
		))
		r.radosnamespaceName = fmt.Sprintf("cephradosnamespace-%s", hex.EncodeToString(rnsMd5Sum[:16]))
		cephFsMd5Sum := md5.Sum([]byte(getStorageRequestName(
			string(r.storageConsumer.UID),
			"ocs-storagecluster-cephfs"),
		))
		r.subVolumeGroupName = fmt.Sprintf("cephfilesystemsubvolumegroup-%s", hex.EncodeToString(cephFsMd5Sum[:16]))
	} else {
		// remote client
		r.radosnamespaceName = string(r.storageConsumer.UID)
		r.subVolumeGroupName = string(r.storageConsumer.UID)
	}

	return nil
}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {
	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring
		r.storageConsumer.Status.CephResources = []*ocsv1alpha1.CephResourcesSpec{}

		if err := r.initPhase(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephRadosNamespace(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileCephFilesystemSubVolumeGroup(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.reconcileNoobaaAccount(); err != nil {
			return reconcile.Result{}, err
		}

		if err := r.consolidateStatus(); err != nil {
			return reconcile.Result{}, err
		}
	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting
	}

	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) reconcileCephRadosNamespace() error {
	blockPools := &rookCephv1.CephBlockPoolList{}
	if err := r.List(r.ctx, blockPools, client.InNamespace(r.namespace)); err != nil {
		return err
	}

	// ensure for this consumer a rados namespace is created in every blockpool
	for idx := range blockPools.Items {
		bp := &blockPools.Items[idx]
		if bp.Name == "builtin-mgr" {
			continue
		}

		rns := &rookCephv1.CephBlockPoolRadosNamespace{}
		rns.Name = fmt.Sprintf("%s-%s", bp.Name, r.radosnamespaceName)
		// Day 1 cephBlockPool have storageCluster name as prefix
		// Old Provider Mode Cluster will continue to use the same radosnamespace CR name on Day 1 cephBlockPool
		if r.isUpdatedProvider && strings.HasPrefix(bp.Name, r.storageCluster.Name) {
			rns.Name = r.radosnamespaceName
		}
		rns.Namespace = r.namespace

		if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, rns, func() error {
			// if rns was created in provider mode storagerequest will be the controller owner
			// as only single controller owner is permitted remove the old and place consumer as the owner
			if existing := metav1.GetControllerOfNoCopy(rns); existing != nil &&
				existing.Kind != r.storageConsumer.Kind {
				existing.BlockOwnerDeletion = nil
				existing.Controller = nil
			}
			if err := r.own(rns); err != nil {
				return err
			}
			rns.Spec.Name = r.radosnamespaceName
			return nil
		}); err != nil {
			return err
		}

		phase := ""
		if rns.Status != nil {
			phase = string(rns.Status.Phase)
		}
		r.setCephResourceStatus(rns.Name, "CephBlockPoolRadosNamespace", phase)
	}

	//TODO: Create the cephClients
	return nil
}

func (r *StorageConsumerReconciler) reconcileCephFilesystemSubVolumeGroup() error {
	cephFs := &rookCephv1.CephFilesystem{}
	cephFs.Name = fmt.Sprintf("%s-%s", r.storageCluster.Name, "cephfilesystem")
	cephFs.Namespace = r.namespace
	if err := r.get(cephFs); err != nil {
		return fmt.Errorf("failed to get CephFilesystem: %v", err)
	}

	svg := &rookCephv1.CephFilesystemSubVolumeGroup{}
	svg.Name = fmt.Sprintf("%s-%s", cephFs.Name, r.subVolumeGroupName)
	if r.isUpdatedProvider {
		svg.Name = r.subVolumeGroupName
	}
	svg.Namespace = r.namespace

	if _, err := ctrl.CreateOrUpdate(r.ctx, r.Client, svg, func() error {
		// if svg was created in provider mode storagerequest will be the controller owner
		// as only single controller owner is permitted remove the old and place consumer as the owner
		if existing := metav1.GetControllerOfNoCopy(svg); existing != nil &&
			existing.Kind != r.storageConsumer.Kind {
			existing.BlockOwnerDeletion = nil
			existing.Controller = nil
		}
		if err := r.own(svg); err != nil {
			return err
		}
		svg.Spec.FilesystemName = cephFs.Name
		svg.Spec.Name = r.subVolumeGroupName
		return nil
	}); err != nil {
		return err
	}

	phase := ""
	if svg.Status != nil {
		phase = string(svg.Status.Phase)
	}
	r.setCephResourceStatus(svg.Name, "CephFilesystemSubVolumeGroup", phase)

	//TODO: Create the cephClients
	return nil
}

func (r *StorageConsumerReconciler) reconcileNoobaaAccount() error {
	if r.isLocalConsumer {
		return nil
	}
	noobaaAccount := &nbv1.NooBaaAccount{}
	noobaaAccount.Name = r.storageConsumer.Name
	noobaaAccount.Namespace = r.storageConsumer.Namespace
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, noobaaAccount, func() error {
		if err := r.own(noobaaAccount); err != nil {
			return err
		}
		// TODO: query the name of backing store during runtime
		noobaaAccount.Spec.DefaultResource = "noobaa-default-backing-store"
		// the following annotation will enable noobaa-operator to create a auth_token secret based on this account
		util.AddAnnotation(noobaaAccount, "remote-operator", "true")
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create noobaa account for storageConsumer %v: %v", r.storageConsumer.Name, err)
	}

	phase := string(noobaaAccount.Status.Phase)
	r.setCephResourceStatus(noobaaAccount.Name, "NooBaaAccount", phase)
	return nil
}

func (r *StorageConsumerReconciler) consolidateStatus() error {
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
	return nil
}

func (r *StorageConsumerReconciler) setCephResourceStatus(name string, kind string, phase string) {
	cephResourceSpec := &ocsv1alpha1.CephResourcesSpec{
		Name:  name,
		Kind:  kind,
		Phase: phase,
	}
	r.storageConsumer.Status.CephResources = append(
		r.storageConsumer.Status.CephResources,
		cephResourceSpec,
	)
}

func (r *StorageConsumerReconciler) get(obj client.Object) error {
	return r.Client.Get(r.ctx, client.ObjectKeyFromObject(obj), obj)
}

func (r *StorageConsumerReconciler) own(resource metav1.Object) error {
	// Ensure storageConsumer ownership on a resource
	return ctrl.SetControllerReference(r.storageConsumer, resource, r.Scheme)
}

func GenerateHashForCephClient(storageConsumerName, cephUserType string) string {
	var c struct {
		StorageConsumerName string `json:"id"`
		CephUserType        string `json:"cephUserType"`
	}

	c.StorageConsumerName = storageConsumerName
	c.CephUserType = cephUserType

	cephClient, err := json.Marshal(c)
	if err != nil {
		klog.Errorf("failed to marshal ceph client name for consumer %s. %v", storageConsumerName, err)
		panic("failed to marshal")
	}
	name := md5.Sum([]byte(cephClient))
	return hex.EncodeToString(name[:16])
}

// Methods to support Upgraded Providers
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
		klog.Errorf("failed to marshal a name for a storage class request based on %v. %v", s, err)
		panic("failed to marshal storage class request name")
	}
	md5Sum := md5.Sum(requestName)
	return hex.EncodeToString(md5Sum[:16])
}

// getStorageRequestName generates a name for a StorageRequest resource.
func getStorageRequestName(consumerUUID, storageClaimName string) string {
	return fmt.Sprintf("storagerequest-%s", getStorageRequestHash(consumerUUID, storageClaimName))
}
