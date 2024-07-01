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
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	providerClient "github.com/red-hat-storage/ocs-operator/v4/services/provider/client"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	rbdMirrorBootstrapPeerSecretName = "rbdMirrorBootstrapPeerSecretName"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger

	ctx                context.Context
	storageClusterPeer *ocsv1.StorageClusterPeer
	cephBlockPoolList  *rookCephv1.CephBlockPoolList
	clusterID          string
	ocsClient          *providerClient.OCSProviderClient
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/finalizers,verbs=update
//+kubebuilder:rbac:groups=ceph.rook.io,resources=cephblockpools;,verbs=list;create;update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *StorageClusterPeerReconciler) Reconcile(ctx context.Context, request ctrl.Request) (ctrl.Result, error) {

	r.ctx = ctrllog.IntoContext(ctx, r.Log)
	r.Log.Info("Reconciling StorageClusterPeer.")

	// Fetch the StorageClusterPeer instance
	r.storageClusterPeer = &ocsv1.StorageClusterPeer{}
	r.storageClusterPeer.Name = request.Name
	r.storageClusterPeer.Namespace = request.Namespace

	if err := r.get(r.storageClusterPeer); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("StorageClusterPeer resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Failed to get StorageClusterPeer.")
		return reconcile.Result{}, err
	}

	r.storageClusterPeer.Status.Phase = ocsv1.StorageClusterPeerInitializing

	r.cephBlockPoolList = &rookCephv1.CephBlockPoolList{}
	if err := r.list(r.cephBlockPoolList, client.InNamespace(r.storageClusterPeer.Namespace)); err != nil {
		return reconcile.Result{}, err
	}

	r.clusterID = util.GetClusterID(r.ctx, r.Client, &r.Log)
	if r.clusterID == "" {
		err := fmt.Errorf("failed to get clusterID from the ClusterVersion CR")
		r.Log.Error(err, "failed to get ClusterVersion of the OCP Cluster")
		return reconcile.Result{}, err
	}
	var err error

	r.ocsClient, err = r.newExternalClient()
	if err != nil {
		return reconcile.Result{}, err
	}
	defer r.ocsClient.Close()

	var result reconcile.Result
	var reconcileError error

	result, reconcileError = r.reconcilePhases()

	// Apply status changes to the StorageClusterPeer
	statusError := r.Client.Status().Update(r.ctx, r.storageClusterPeer)
	if statusError != nil {
		r.Log.Info("Failed to update StorageClusterPeer status.")
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

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {

	enqueueStorageClusterPeerRequest := handler.EnqueueRequestsFromMapFunc(
		func(ctx context.Context, obj client.Object) []reconcile.Request {
			// Get the StorageClusterPeer objects
			scpList := &ocsv1.StorageClusterPeerList{}
			err := r.Client.List(ctx, scpList, &client.ListOptions{Namespace: obj.GetNamespace()})
			if err != nil {
				r.Log.Error(err, "Unable to list StorageClusterPeer objects")
				return []reconcile.Request{}
			}

			// Return name and namespace of the StorageClusterPeers object
			request := []reconcile.Request{}
			for _, scp := range scpList.Items {
				request = append(request, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Namespace: scp.Namespace,
						Name:      scp.Name,
					},
				})
			}

			return request
		},
	)

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageClusterPeer{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(&rookCephv1.CephBlockPool{}, enqueueStorageClusterPeerRequest).
		Complete(r)
}

func (r *StorageClusterPeerReconciler) reconcilePhases() (reconcile.Result, error) {
	r.Log.Info("Running StorageClusterPeer controller")

	//marked for deletion
	if !r.storageClusterPeer.GetDeletionTimestamp().IsZero() {
		r.storageClusterPeer.Status.Phase = ocsv1.StorageClusterPeerDeleting

		if err := r.reconcileRevokeBlockPoolPeering(); err != nil {
			return reconcile.Result{}, err
		}

		//remove finalizer
		if controllerutil.RemoveFinalizer(r.storageClusterPeer, ocsv1.StorageClusterPeerFinalizer) {
			r.Log.Info("removing finalizer from the StorageClusterPeer, ", "StorageClusterPeer", r.storageClusterPeer.Name)

			if err := r.update(r.storageClusterPeer); err != nil {
				r.Log.Info("Failed to remove finalizer from StorageClusterPeer", "StorageClusterPeer", r.storageClusterPeer.Name)
				return reconcile.Result{}, fmt.Errorf("failed to update StorageClusterPeer %q: %v", r.storageClusterPeer.Name, err)
			}
		}
		r.Log.Info("finalizer removed successfully")
		return reconcile.Result{}, nil
	}
	//not marked for deletion

	// add finalizer
	if controllerutil.AddFinalizer(r.storageClusterPeer, ocsv1.StorageClusterPeerFinalizer) {
		r.storageClusterPeer.Status.Phase = ocsv1.StorageClusterPeerInitializing
		r.Log.Info("Finalizer not found for StorageClusterPeer. Adding finalizer.", "StorageClusterPeer", r.storageClusterPeer.Name)
		if err := r.update(r.storageClusterPeer); err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to update StorageClusterPeer %q: %v", r.storageClusterPeer.Name, err)
		}
	}

	r.storageClusterPeer.Status.Phase = ocsv1.StorageClusterPeerConfiguring

	if err := r.reconcileLocalBlockPools(); err != nil {
		return reconcile.Result{}, err
	}

	if err := r.reconcilePeerBlockPools(); err != nil {
		return reconcile.Result{}, err
	}

	r.storageClusterPeer.Status.Phase = ocsv1.StorageClusterPeerReady

	return reconcile.Result{}, nil
}

func (r *StorageClusterPeerReconciler) newExternalClient() (*providerClient.OCSProviderClient, error) {
	ocsClient, err := providerClient.NewProviderClient(r.ctx, r.storageClusterPeer.Spec.OCSAPIServerURI, time.Second*10)
	if err != nil {
		return nil, fmt.Errorf("failed to create a new ocs client: %v", err)
	}
	return ocsClient, nil
}

func (r *StorageClusterPeerReconciler) reconcileLocalBlockPools() error {

	for _, cephBlockPool := range r.cephBlockPoolList.Items {

		_, err := ctrl.CreateOrUpdate(r.ctx, r.Client, &cephBlockPool, func() error {
			cephBlockPool.Spec.Mirroring.Enabled = true
			cephBlockPool.Spec.Mirroring.Mode = "image"
			return nil
		})
		if err != nil {
			r.Log.Error(err, "unable to enable mirroring on the cephBlockPool",
				"CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			return err
		}
	}
	return nil
}

func (r *StorageClusterPeerReconciler) reconcilePeerBlockPools() error {

	for _, cephBlockPool := range r.cephBlockPoolList.Items {

		if cephBlockPool.Status.Info == nil || cephBlockPool.Status.Info[rbdMirrorBootstrapPeerSecretName] == "" {
			r.Log.Info("waiting for bootstrap secret to be generated",
				"CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			return fmt.Errorf("waiting for bootstrap secret for %s blockpool in %s namespace to be generated",
				cephBlockPool.Name, cephBlockPool.Namespace)
		}

		secret := &v1.Secret{}
		secret.Name = cephBlockPool.Status.Info[rbdMirrorBootstrapPeerSecretName]
		secret.Namespace = cephBlockPool.Namespace

		if err := r.get(secret); err != nil {
			r.Log.Error(err, "unable to fetch the bootstrap secret for blockPool",
				"CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			return err
		}

		_, err := r.ocsClient.PeerBlockPool(r.ctx, generateSecretName(cephBlockPool.Name, r.clusterID), secret.Data["pool"], secret.Data["token"])
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *StorageClusterPeerReconciler) reconcileRevokeBlockPoolPeering() error {

	for _, cephBlockPool := range r.cephBlockPoolList.Items {

		_, err := r.ocsClient.RevokeBlockPoolPeering(r.ctx, generateSecretName(cephBlockPool.Name, r.clusterID), []byte(cephBlockPool.Name))
		if err != nil {
			return err
		}

		_, err = ctrl.CreateOrUpdate(r.ctx, r.Client, &cephBlockPool, func() error {
			// if bootstrap secret is set then it might be used as a backup for some other cluster
			if cephBlockPool.Spec.Mirroring.Peers == nil || len(cephBlockPool.Spec.Mirroring.Peers.SecretNames) == 0 {
				cephBlockPool.Spec.Mirroring.Enabled = false
				cephBlockPool.Spec.Mirroring.Mode = ""
			}
			return nil
		})
		if err != nil {
			r.Log.Error(err, "unable to disable mirroring on the cephBlockPool",
				"CephBlockPool", klog.KRef(cephBlockPool.Namespace, cephBlockPool.Name))
			return err
		}
	}
	return nil
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

func generateSecretName(poolName, clusterID string) string {
	md5Sum := md5.Sum([]byte(fmt.Sprintf("%s-%s", poolName, clusterID)))
	return fmt.Sprintf("bootstrap-secret-%s", hex.EncodeToString(md5Sum[:16]))
}
