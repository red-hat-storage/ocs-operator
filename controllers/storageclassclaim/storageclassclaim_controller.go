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

	v1 "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/controllers/storagecluster"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageClassClaimReconciler reconciles a StorageClassClaim object
// nolint
type StorageClassClaimReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclassclaims/status,verbs=get;update;patch

func (r *StorageClassClaimReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx, "StorageClassClaim", request)
	ctx = ctrllog.IntoContext(ctx, log)
	log.Info("Reconciling StorageClassClaim.")

	// Fetch the StorageClassClaim instance
	claim := &ocsv1alpha1.StorageClassClaim{}
	if err := r.Client.Get(ctx, request.NamespacedName, claim); err != nil {
		if errors.IsNotFound(err) {
			log.Info("StorageClassClaim resource not found. Ignoring since object must be deleted.")
			return reconcile.Result{}, nil
		}
		log.Error(err, "Failed to get StorageClassClaim.")
		return reconcile.Result{}, err
	}

	storageClusterList := &v1.StorageClusterList{}
	if err := r.Client.List(ctx, storageClusterList); err != nil {
		return reconcile.Result{}, err
	}

	if len(storageClusterList.Items) > 1 {
		return reconcile.Result{}, fmt.Errorf("multiple StorageCluster found")
	}

	var result reconcile.Result
	var reconcileError error
	if storagecluster.IsOCSConsumerMode(&storageClusterList.Items[0]) {
		result, reconcileError = r.reconcileConsumerPhases(ctx, &storageClusterList.Items[0])
	} else {
		result, reconcileError = r.reconcileProviderPhases(ctx, &storageClusterList.Items[0])
	}

	// Apply status changes to the StorageClassClaim
	statusError := r.Client.Status().Update(ctx, claim)
	if statusError != nil {
		log.Info("Failed to update StorageClassClaim status.")
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

func (r *StorageClassClaimReconciler) reconcileConsumerPhases(ctx context.Context, storagecluster *v1.StorageCluster) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Running StorageClassClaim controller in Consumer Mode")
	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) reconcileProviderPhases(ctx context.Context, storagecluster *v1.StorageCluster) (reconcile.Result, error) {
	log := ctrllog.FromContext(ctx)
	log.Info("Running StorageClassClaim controller in Converged/Provider Mode")
	return reconcile.Result{}, nil
}

func (r *StorageClassClaimReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1alpha1.StorageClassClaim{}).
		Complete(r)
}
