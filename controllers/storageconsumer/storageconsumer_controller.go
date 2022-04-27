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
	"github.com/red-hat-storage/ocs-operator/api/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/source"

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storageConsumerFinalizer      = "storagesconsumer.ocs.openshift.io"
	StorageConsumerAnnotation     = "ocs.openshift.io.storageconsumer"
	StorageClaimAnnotation        = "ocs.openshift.io.storageclaim"
	StorageCephUserTypeAnnotation = "ocs.openshift.io.cephusertype"
	ConsumerUUIDLabel             = "ocs.openshift.io/storageconsumer-uuid"
	StorageConsumerNameLabel      = "ocs.openshift.io/storageconsumer-name"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx                     context.Context
	storageConsumer         *ocsv1alpha1.StorageConsumer
	cephClientHealthChecker *rookCephv1.CephClient
	cephResourcesByName     map[string]*ocsv1alpha1.CephResourcesSpec
	namespace               string
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
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

	r.cephClientHealthChecker = &rookCephv1.CephClient{}
	r.cephClientHealthChecker.Name = GenerateHashForCephClient(r.storageConsumer.Name, "global")
	r.cephClientHealthChecker.Namespace = r.namespace
}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {

	if !r.storageConsumer.Spec.Enable {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDisabled
		return reconcile.Result{}, nil
	}

	r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring

	for _, cephResourceSpec := range r.storageConsumer.Status.CephResources {
		r.cephResourcesByName[cephResourceSpec.Name] = cephResourceSpec
	}

	if r.storageConsumer.GetDeletionTimestamp().IsZero() {
		if !Contains(r.storageConsumer.GetFinalizers(), storageConsumerFinalizer) {
			r.Log.Info("Finalizer not found for StorageConsumer. Adding finalizer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
			r.storageConsumer.ObjectMeta.Finalizers = append(r.storageConsumer.ObjectMeta.Finalizers, storageConsumerFinalizer)
			if err := r.Client.Update(r.ctx, r.storageConsumer); err != nil {
				r.Log.Error(err, "Failed to update StorageConsumer with finalizer.", "StorageConsumer", klog.KRef(r.storageConsumer.Namespace, r.storageConsumer.Name))
				return reconcile.Result{}, err
			}
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

		cephBlockPoolList := &rookCephv1.CephBlockPoolList{}
		cephBlockPoolListOption := []client.ListOption{
			client.InNamespace(r.namespace),
			client.MatchingLabels(map[string]string{StorageConsumerNameLabel: r.storageConsumer.Name}),
		}

		if err := r.list(cephBlockPoolList, cephBlockPoolListOption); err != nil && len(cephBlockPoolList.Items) > 0 {
			r.storageConsumer.Status.GrantedCapacity = r.storageConsumer.Spec.Capacity
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
				}
			}
		}
	}

	return reconcile.Result{}, nil
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
func Contains(slice []string, s string) bool {
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
	enqueueStorageConsumerRequest := handler.EnqueueRequestsFromMapFunc(
		func(obj client.Object) []reconcile.Request {
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
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&rookCephv1.CephClient{}).
		// Watch non-owned resources cephBlockPool
		// We are required to update the storageConsumers `GrantedCapacity`.
		// Whenever their is new cephBockPool created to keep storageConsumer upto date.
		Watches(
			&source.Kind{Type: &rookCephv1.CephBlockPool{}},
			enqueueStorageConsumerRequest,
		).
		Complete(r)
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

func (r *StorageConsumerReconciler) list(obj client.ObjectList, listOpt []client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOpt...)
}
