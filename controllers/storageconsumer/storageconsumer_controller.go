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
	"k8s.io/utils/ptr"
	"strings"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	"github.com/go-logr/logr"
	appsv1 "k8s.io/api/apps/v1"
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
	StorageConsumerAnnotation     = "ocs.openshift.io.storageconsumer"
	StorageRequestAnnotation      = "ocs.openshift.io.storagerequest"
	StorageCephUserTypeAnnotation = "ocs.openshift.io.cephusertype"
	StorageProfileLabel           = "ocs.openshift.io/storageprofile"
	ConsumerUUIDLabel             = "ocs.openshift.io/storageconsumer-uuid"
	StorageConsumerNameLabel      = "ocs.openshift.io/storageconsumer-name"
	MaintenanceModeAnnotation     = "ocs.openshift.io/maintenanceMode"
)

// StorageConsumerReconciler reconciles a StorageConsumer object
type StorageConsumerReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	ctx             context.Context
	storageConsumer *ocsv1alpha1.StorageConsumer
	//storageConsumerList *ocsv1alpha1.StorageConsumerList
	namespace     string
	noobaaAccount *nbv1.NooBaaAccount
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageconsumers/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storagerequests,verbs=get;list;
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaaaccounts,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;update

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

	r.noobaaAccount = &nbv1.NooBaaAccount{}
	r.noobaaAccount.Name = r.storageConsumer.Name
	r.noobaaAccount.Namespace = r.storageConsumer.Namespace
}

func (r *StorageConsumerReconciler) reconcilePhases() (reconcile.Result, error) {

	if !r.storageConsumer.Spec.Enable {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDisabled
		return reconcile.Result{}, nil
	}

	r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateConfiguring
	r.storageConsumer.Status.CephResources = []*ocsv1alpha1.CephResourcesSpec{}

	if r.storageConsumer.GetDeletionTimestamp().IsZero() {

		// A provider cluster already has a NooBaa system and does not require a NooBaa account
		// to connect to a remote cluster, unlike client clusters.
		// A NooBaa account only needs to be created if the storage consumer is for a client cluster.
		clusterID := util.GetClusterID(r.ctx, r.Client, &r.Log)
		if clusterID != "" && !strings.Contains(r.storageConsumer.Name, clusterID) {
			if err := r.reconcileNoobaaAccount(); err != nil {
				return reconcile.Result{}, err
			}
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

		if err := r.reconcileMaintenanceMode(); err != nil {
			return reconcile.Result{}, err
		}

	} else {
		r.storageConsumer.Status.State = v1alpha1.StorageConsumerStateDeleting
	}

	return reconcile.Result{}, nil
}

func (r *StorageConsumerReconciler) reconcileNoobaaAccount() error {
	_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, r.noobaaAccount, func() error {
		if err := r.own(r.noobaaAccount); err != nil {
			return err
		}
		// TODO: query the name of backing store during runtime
		r.noobaaAccount.Spec.DefaultResource = "noobaa-default-backing-store"
		// the following annotation will enable noobaa-operator to create a auth_token secret based on this account
		util.AddAnnotation(r.noobaaAccount, "remote-operator", "true")
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create noobaa account for storageConsumer %v: %v", r.storageConsumer.Name, err)
	}

	phase := string(r.noobaaAccount.Status.Phase)
	r.setCephResourceStatus(r.noobaaAccount.Name, "NooBaaAccount", phase, nil)

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

	rbdMirrorPredicate := builder.WithPredicates(
		predicate.NewPredicateFuncs(
			func(client client.Object) bool {
				return client.GetLabels()["app"] == "rook-ceph-rbd-mirror"
			},
		),
		predicate.GenerationChangedPredicate{},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1alpha1.StorageConsumer{}, builder.WithPredicates(
			predicate.GenerationChangedPredicate{},
		)).
		Owns(&nbv1.NooBaaAccount{}).
		// Watch non-owned resources cephBlockPool
		// Whenever their is new cephBockPool created to keep storageConsumer up to date.
		Watches(&rookCephv1.CephBlockPool{},
			enqueueStorageConsumerRequest,
		).
		Watches(&appsv1.Deployment{}, enqueueStorageConsumerRequest, rbdMirrorPredicate).
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

func (r *StorageConsumerReconciler) reconcileMaintenanceMode() error {
	rbdMirrorDeployments := &appsv1.DeploymentList{}
	err := r.List(
		r.ctx,
		rbdMirrorDeployments,
		client.InNamespace(r.storageConsumer.Namespace),
		client.MatchingLabels{"app": "rook-ceph-rbd-mirror"},
		client.Limit(1),
	)
	if err != nil {
		return err
	}
	if len(rbdMirrorDeployments.Items) == 0 {
		return nil
	}

	replicas := int32(1)
	enableMaintenanceMode, err := r.enableMaintenanceMode()
	if err != nil {
		return err
	}
	if enableMaintenanceMode {
		replicas = 0
	}

	rbdMirrorDeployment := &rbdMirrorDeployments.Items[0]
	rbdMirrorDeployment.Spec.Replicas = ptr.To(replicas)
	err = r.Update(r.ctx, rbdMirrorDeployment)
	if err != nil {
		return err
	}

	return nil
}

func (r *StorageConsumerReconciler) enableMaintenanceMode() (bool, error) {
	storageConsumerList := &ocsv1alpha1.StorageConsumerList{}
	err := r.List(r.ctx, storageConsumerList, client.InNamespace(r.storageConsumer.Namespace))
	if err != nil {
		return false, err
	}
	for i := range storageConsumerList.Items {
		if _, ok := storageConsumerList.Items[i].GetAnnotations()[MaintenanceModeAnnotation]; ok {
			return true, nil
		}
	}
	return false, nil
}
