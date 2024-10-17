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

package storageclusterpeer

import (
	"context"
	"fmt"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/status"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	storageClusterPeerFinalizer = "storageclusterpeer.ocs.openshift.io"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	log                logr.Logger
	ctx                context.Context
	storageClusterPeer *ocsv1.StorageClusterPeer
	storageCluster     *ocsv1.StorageCluster
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageClusterPeer{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusters,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *StorageClusterPeerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {
	var err error
	r.ctx = ctx
	r.log = log.FromContext(ctx, "StorageClient", request)
	r.log.Info("Reconciling StorageClusterPeer.")

	// Fetch StorageCluster(s)
	storageClusterList := &ocsv1.StorageClusterList{}
	err = r.list(storageClusterList, client.InNamespace(request.Namespace))
	if err != nil {
		r.log.Error(err, "StorageCluster for StorageClusterPeer found in the same namespace.")
		return ctrl.Result{}, err
	}

	if len(storageClusterList.Items) != 1 {
		err := fmt.Errorf("invalid number of StorageCluster found")
		r.log.Error(err, "invalid number of StorageCluster(s) found, expected 1, found %v", len(storageClusterList.Items))
		return ctrl.Result{}, err
	}

	r.storageCluster = &storageClusterList.Items[0]

	// Fetch the StorageClusterPeer instance
	r.storageClusterPeer = &ocsv1.StorageClusterPeer{}
	r.storageClusterPeer.Name = request.Name
	r.storageClusterPeer.Namespace = request.Namespace

	if err = r.get(r.storageClusterPeer); err != nil {
		if errors.IsNotFound(err) {
			r.log.Info("StorageClusterPeer resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageClusterPeer.")
		return reconcile.Result{}, err
	}

	result, reconcileErr := r.reconcilePhases()

	// Apply status changes to the StorageClient
	statusErr := r.Client.Status().Update(ctx, r.storageClusterPeer)
	if statusErr != nil {
		r.log.Error(statusErr, "Failed to update StorageClusterPeer status.")
	}

	if reconcileErr != nil {
		err = reconcileErr
	} else if statusErr != nil {
		err = statusErr
	}

	return result, err
}

func (r *StorageClusterPeerReconciler) reconcilePhases() (ctrl.Result, error) {
	ocsClient, err := r.newExternalClusterClient()
	if err != nil {
		return reconcile.Result{}, err
	}
	defer ocsClient.Close()

	// marked for deletion
	if !r.storageClusterPeer.GetDeletionTimestamp().IsZero() {
		//TODO: Removing PeerOCS Call

		if controllerutil.RemoveFinalizer(r.storageClusterPeer, storageClusterPeerFinalizer) {
			r.log.Info("removing finalizer from StorageClusterPeer.", "StorageClusterPeer", r.storageClusterPeer.Name)
			if err := r.update(r.storageClusterPeer); err != nil {
				r.log.Info("Failed to remove finalizer from StorageClusterPeer", "StorageClusterPeer", r.storageClusterPeer.Name)
				return reconcile.Result{}, fmt.Errorf("failed to remove finalizer from StorageClient: %v", err)
			}
		}
	}

	if controllerutil.AddFinalizer(r.storageClusterPeer, storageClusterPeerFinalizer) {
		r.log.Info("Finalizer not found for StorageClusterPeer. Adding finalizer.", "StorageClusterPeer", r.storageClusterPeer.Name)
		if err := r.update(r.storageClusterPeer); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update StorageClusterPeer: %v", err)
		}
	}

	if r.storageClusterPeer.Status.State != ocsv1.StorageClusterPeerRemoteStatePeered {
		return r.peerOCS(ocsClient)
	}

	return ctrl.Result{}, nil
}

func (r *StorageClusterPeerReconciler) newExternalClusterClient() (*providerClient.OCSProviderClient, error) {

	ocsProviderClient, err := providerClient.NewProviderClient(
		r.ctx, r.storageClusterPeer.Spec.ApiEndpoint, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new provider client: %v", err)
	}

	return ocsProviderClient, nil
}

func (r *StorageClusterPeerReconciler) peerOCS(ocsClient *providerClient.OCSProviderClient) (ctrl.Result, error) {
	r.storageClusterPeer.Status.State = ocsv1.StorageClusterPeerRemoteStatePeering
	response, err := ocsClient.PeerStorageCluster(r.ctx, r.storageClusterPeer.Spec.OnboardingToken, string(r.storageCluster.UID))
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to Peer Storage Cluster, code: %v.", status.Code(err)))
		return ctrl.Result{}, err
	}
	if r.storageClusterPeer.Status.RemoteStorageClusterUID != response.StorageClusterUID {
		err := fmt.Errorf("falied to validate remote Storage Cluster UID against PeerOCS Response")
		r.log.Error(err, "failed to Peer Storage Cluster")
		return ctrl.Result{}, err
	}
	r.storageClusterPeer.Status.State = ocsv1.StorageClusterPeerRemoteStatePeered
	return ctrl.Result{}, nil
}

func (r *StorageClusterPeerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}

func (r *StorageClusterPeerReconciler) list(obj client.ObjectList, listOptions ...client.ListOption) error {
	return r.Client.List(r.ctx, obj, listOptions...)
}

func (r *StorageClusterPeerReconciler) update(obj client.Object, opts ...client.UpdateOption) error {
	return r.Client.Update(r.ctx, obj, opts...)
}
