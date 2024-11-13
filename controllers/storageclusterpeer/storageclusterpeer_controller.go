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

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
)

const (
	OcsClientPeerConfigMapName = "ocs-client-peer-mapping"
)

// StorageClusterPeerReconciler reconciles a StorageClusterPeer object
// nolint:revive
type StorageClusterPeerReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=ocs.openshift.io,resources=storageclusterpeers/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the StorageClusterPeer object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *StorageClusterPeerReconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *StorageClusterPeerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.StorageClusterPeer{}).
		Complete(r)
}
