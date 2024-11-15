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
	"encoding/base64"
	"encoding/json"
	"fmt"
	"google.golang.org/grpc/codes"
	"strings"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	providerClient "github.com/red-hat-storage/ocs-operator/services/provider/api/v4/client"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services"

	"github.com/go-logr/logr"
	"google.golang.org/grpc/status"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	log logr.Logger
	ctx context.Context
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageClusterPeer{}).
		Watches(&ocsv1.StorageCluster{}, &handler.EnqueueRequestForObject{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
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

	// Fetch the StorageClusterPeer instance
	storageClusterPeer := &ocsv1.StorageClusterPeer{}
	storageClusterPeer.Name = request.Name
	storageClusterPeer.Namespace = request.Namespace

	if err = r.get(storageClusterPeer); err != nil {
		if k8serrors.IsNotFound(err) {
			r.log.Info("StorageClusterPeer resource not found. Ignoring since object must have been deleted.")
			return ctrl.Result{}, nil
		}
		r.log.Error(err, "Failed to get StorageClusterPeer.")
		return ctrl.Result{}, err
	}

	if storageClusterPeer.Status.State == ocsv1.StorageClusterPeerStatePeered {
		return ctrl.Result{}, nil
	}

	result, reconcileError := r.reconcileStates(storageClusterPeer)

	// Apply status changes
	statusError := r.Client.Status().Update(r.ctx, storageClusterPeer)
	if statusError != nil {
		r.log.Info("Could not update StorageClusterPeer status.")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	}

	return result, nil
}

func (r *StorageClusterPeerReconciler) reconcileStates(storageClusterPeer *ocsv1.StorageClusterPeer) (ctrl.Result, error) {
	storageCluster := &ocsv1.StorageCluster{}
	storageCluster.Namespace = storageClusterPeer.Namespace

	// Fetch StorageCluster
	owner := util.FindOwnerRefByKind(storageClusterPeer, "StorageCluster")

	if owner == nil {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		return ctrl.Result{}, fmt.Errorf("failed to find StorgeCluster owning the StorageClusterPeer")
	}

	storageCluster.Name = owner.Name

	if err := r.get(storageCluster); client.IgnoreNotFound(err) != nil {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		r.log.Error(err, "Error fetching StorageCluster for StorageClusterPeer found in the same namespace.")
		return ctrl.Result{}, err
	} else if k8serrors.IsNotFound(err) {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		r.log.Error(err, "Cannot find a StorageCluster for StorageClusterPeer in the same namespace.")
		return ctrl.Result{}, nil
	}

	storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateInitializing

	// Read StorageClusterUID from ticket
	ticketArr := strings.Split(string(storageClusterPeer.Spec.OnboardingToken), ".")
	if len(ticketArr) != 2 {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		r.log.Error(fmt.Errorf("invalid ticket"), "Invalid onboarding ticket. Does not conform to expected ticket structure")
		return ctrl.Result{}, nil
	}
	message, err := base64.StdEncoding.DecodeString(ticketArr[0])
	if err != nil {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		r.log.Error(err, "failed to decode onboarding ticket")
		return ctrl.Result{}, nil
	}
	var ticketData services.OnboardingTicket
	err = json.Unmarshal(message, &ticketData)
	if err != nil {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		r.log.Error(err, "onboarding ticket message is not a valid JSON.")
		return ctrl.Result{}, nil
	}

	if storageClusterPeer.Status.PeerInfo == nil {
		storageClusterPeer.Status.PeerInfo = &ocsv1.PeerInfo{StorageClusterUid: string(ticketData.StorageCluster)}
	}

	ocsClient, err := providerClient.NewProviderClient(r.ctx, storageClusterPeer.Spec.ApiEndpoint, time.Second*10)
	if err != nil {
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		return ctrl.Result{}, fmt.Errorf("failed to create a new provider client: %v", err)
	}
	defer ocsClient.Close()

	storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStatePending

	_, err = ocsClient.PeerStorageCluster(r.ctx, storageClusterPeer.Spec.OnboardingToken, string(storageCluster.UID))
	if err != nil {
		r.log.Error(err, fmt.Sprintf("failed to Peer Storage Cluster, reason: %v.", err))
		st, ok := status.FromError(err)
		if !ok {
			r.log.Error(fmt.Errorf("invalid code"), "failed to extract an HTTP status code from error")
			return ctrl.Result{}, fmt.Errorf("failed to extract an HTTP status code from error")
		}
		if st.Code() != codes.InvalidArgument {
			return ctrl.Result{}, err
		}
		storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStateFailed
		return ctrl.Result{}, nil
	}

	storageClusterPeer.Status.State = ocsv1.StorageClusterPeerStatePeered
	return ctrl.Result{}, nil
}

func (r *StorageClusterPeerReconciler) get(obj client.Object) error {
	key := client.ObjectKeyFromObject(obj)
	return r.Client.Get(r.ctx, key, obj)
}
