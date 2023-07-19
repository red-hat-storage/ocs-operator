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

	"github.com/go-logr/logr"
	"github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
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

	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/v4/api/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	StorageConsumerAnnotation     = "ocs.openshift.io.storageconsumer"
	StorageRequestAnnotation      = "ocs.openshift.io.storagerequest"
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
	namespace               string
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephclients,verbs=get;list;watch;create;update;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassrequests,verbs=get;list;

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

	// Initialize the reconciler properties from the request
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
	r.storageConsumer.Status.CephResources = []*ocsv1alpha1.CephResourcesSpec{}

	if r.storageConsumer.GetDeletionTimestamp().IsZero() {

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

func (r *StorageConsumerReconciler) setCephResourceStatus(name string, kind string, phase string, cephClients map[string]string) {
	cephResourceSpec := ocsv1alpha1.CephResourcesSpec{
		Name:        name,
		Kind:        kind,
		Phase:       phase,
		CephClients: cephClients,
	}
	r.storageConsumer.Status.CephResources = append(
		r.storageConsumer.Status.CephResources,
		&cephResourceSpec,
	)
}

func (r *StorageConsumerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageConsumerReconciler) own(resource metav1.Object) error {
	// Ensure storageConsumer ownership on a resource
	return ctrl.SetControllerReference(r.storageConsumer, resource, r.Scheme)
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
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&rookCephv1.CephClient{}).
		// Watch non-owned resources cephBlockPool
		// Whenever their is new cephBockPool created to keep storageConsumer up to date.
		Watches(&rookCephv1.CephBlockPool{},
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

func addStorageRelatedAnnotations(obj client.Object, storageConsumerName, storageRequest, cephUserType string) {
	annotations := obj.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
		obj.SetAnnotations(annotations)
	}

	annotations[StorageConsumerAnnotation] = storageConsumerName
	annotations[StorageRequestAnnotation] = storageRequest
	annotations[StorageCephUserTypeAnnotation] = cephUserType
}
