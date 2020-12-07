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

package ocsinitialization

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	secv1client "github.com/openshift/client-go/security/clientset/versioned/typed/security/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/util"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// watchNamespace is the namespace the operator is watching.
var watchNamespace string

const wrongNamespacedName = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."

// OCSInitializationReconciler reconciles a OCSInitialization object
//nolint
type OCSInitializationReconciler struct {
	client.Client
	Log       logr.Logger
	Scheme    *runtime.Scheme
	SecClient secv1client.SecurityV1Interface
	RookImage string
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=ocsinitializations,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ocs.openshift.io,resources=ocsinitializations/status,verbs=get;update;patch

// Reconcile reads that state of the cluster for a OCSInitialization object and makes changes based on the state read
// and what is in the OCSInitialization.Spec
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *OCSInitializationReconciler) Reconcile(request ctrl.Request) (ctrl.Result, error) {
	_ = context.Background()
	_ = r.Log.WithValues("ocsinitialization", request.NamespacedName)

	r.Log.Info("Reconciling OCSInitialization")

	initNamespacedName := InitNamespacedName()
	instance := &ocsv1.OCSInitialization{}
	if initNamespacedName.Name != request.Name || initNamespacedName.Namespace != request.Namespace {
		// Ignoring this resource because it has the wrong name or namespace
		r.Log.Info(wrongNamespacedName)
		err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{}, err
		}

		instance.Status.Phase = util.PhaseIgnored
		err = r.Client.Status().Update(context.TODO(), instance)
		if err != nil {
			r.Log.Error(err, "failed to update ignored resource")
		}
		return ctrl.Result{}, err
	}

	// Fetch the OCSInitialization instance
	err := r.Client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Recreating since we depend on this to exist. A user may delete it to
			// induce a reset of all initial data.
			r.Log.Info("recreating OCSInitialization resource")
			return ctrl.Result{}, r.Client.Create(context.TODO(), &ocsv1.OCSInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      initNamespacedName.Name,
					Namespace: initNamespacedName.Namespace,
				},
			})
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing OCSInitialization resource"
		util.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseProgressing
		err = r.Client.Status().Update(context.TODO(), instance)
		if err != nil {
			r.Log.Error(err, "Failed to add conditions to status")
			return ctrl.Result{}, err
		}
	}

	err = r.ensureSCCs(instance, r.Log)
	if err != nil {
		reason := ocsv1.ReconcileFailed
		message := fmt.Sprintf("Error while reconciling: %v", err)
		util.SetErrorCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = util.PhaseError
		// don't want to overwrite the actual reconcile failure
		uErr := r.Client.Status().Update(context.TODO(), instance)
		if uErr != nil {
			r.Log.Error(uErr, "Failed to update conditions")
		}
		return ctrl.Result{}, err
	}
	instance.Status.SCCsCreated = true

	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	err = r.ensureToolsDeployment(instance)
	if err != nil {
		r.Log.Error(err, "Failed to process ceph tools deployment")
		return ctrl.Result{}, err
	}

	if !instance.Status.RookCephOperatorConfigCreated {
		// if true, no need to ensure presence of ConfigMap
		// if false, ensure ConfigMap and update the status
		err = r.ensureRookCephOperatorConfig(instance)
		if err != nil {
			r.Log.Error(err, "Failed to process %s Configmap", rookCephOperatorConfigName)
			return ctrl.Result{}, err
		}
		instance.Status.RookCephOperatorConfigCreated = true
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	util.SetCompleteCondition(&instance.Status.Conditions, reason, message)

	instance.Status.Phase = util.PhaseReady
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up a controller managed by the manager
func (r *OCSInitializationReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// set the watchNamespace so we know where to create the OCSInitialization resource
	ns, err := util.GetWatchNamespace()
	if err != nil {
		return err
	}
	watchNamespace = ns

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.OCSInitialization{}).
		Owns(&appsv1.Deployment{}).
		Complete(r)
}

// InitNamespacedName returns a NamespacedName for the singleton instance that should exist.
func InitNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      "ocsinit",
		Namespace: watchNamespace,
	}
}
