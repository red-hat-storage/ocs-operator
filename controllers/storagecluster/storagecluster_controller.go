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

package storagecluster

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

// StorageClusterReconciler reconciles a StorageCluster object
//nolint
type StorageClusterReconciler struct {
	client.Client
	Log             logr.Logger
	Scheme          *runtime.Scheme
	ServerVersion   *version.Info
	Conditions      []conditionsv1.Condition
	Phase           string
	CephImage       string
	MonitoringIP    string
	NoobaaDBImage   string
	NoobaaCoreImage string
	NodeCount       int
	Platform        *Platform
}

// SetupWithManager sets up a controller managed by the manager
func (r *StorageClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	err := r.initializeImageVars()
	if err != nil {
		return fmt.Errorf("initializing image variables for %T failed: %v", r, err)
	}
	err = r.initializeServerVersion()
	if err != nil {
		return fmt.Errorf("initializing server version for %T failed: %v", r, err)
	}
	// Compose a predicate that is an OR of the specified predicates
	scPredicate := util.ComposePredicates(
		predicate.GenerationChangedPredicate{},
		util.MetadataChangedPredicate{},
	)

	pvcPredicate := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&ocsv1.StorageCluster{}, builder.WithPredicates(scPredicate)).
		Owns(&cephv1.CephCluster{}).
		Owns(&nbv1.NooBaa{}).
		Owns(&corev1.PersistentVolumeClaim{}, builder.WithPredicates(pvcPredicate)).
		Complete(r)
}

func (r *StorageClusterReconciler) initializeImageVars() error {
	r.CephImage = os.Getenv("CEPH_IMAGE")
	r.NoobaaCoreImage = os.Getenv("NOOBAA_CORE_IMAGE")
	r.NoobaaDBImage = os.Getenv("NOOBAA_DB_IMAGE")

	if r.CephImage == "" {
		err := fmt.Errorf("CEPH_IMAGE environment variable not found")
		r.Log.Error(err, "missing required environment variable for ocs initialization")
		return err
	} else if r.NoobaaCoreImage == "" {
		err := fmt.Errorf("NOOBAA_CORE_IMAGE environment variable not found")
		r.Log.Error(err, "missing required environment variable for ocs initialization")
		return err
	} else if r.NoobaaDBImage == "" {
		err := fmt.Errorf("NOOBAA_DB_IMAGE environment variable not found")
		r.Log.Error(err, "missing required environment variable for ocs initialization")
		return err
	}
	return nil
}

func (r *StorageClusterReconciler) initializeServerVersion() error {
	clientset, err := kubernetes.NewForConfig(config.GetConfigOrDie())
	if err != nil {
		r.Log.Error(err, "failed creation of clientset for determining serverversion")
		return err
	}
	r.ServerVersion, err = clientset.Discovery().ServerVersion()
	if err != nil {
		r.Log.Error(err, "failed getting the serverversion")
		return err
	}
	return nil
}
