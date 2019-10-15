package storagecluster

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_storagecluster")

/**
* USER ACTION REQUIRED: This is a scaffold file intended for the user to modify with their own Controller
* business logic.  Delete these comments after modifying this file.*
 */

// Add creates a new StorageCluster Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {

	cephImage := os.Getenv("CEPH_IMAGE")
	if cephImage == "" {
		err := fmt.Errorf("CEPH_IMAGE environment variable not found")
		log.Error(err, "missing required environment variable for ocs initialization")
		panic(err)
	}

	return &ReconcileStorageCluster{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		reqLogger: log,
		cephImage: cephImage,
	}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// Create a new controller
	c, err := controller.New("storagecluster-controller", mgr, controller.Options{MaxConcurrentReconciles: 1, Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource StorageCluster
	err = c.Watch(&source.Kind{Type: &ocsv1.StorageCluster{}}, &handler.EnqueueRequestForObject{})
	if err != nil {
		return err
	}

	// Watch for changes to secondary resource Pods and requeue the owner StorageCluster

	err = c.Watch(&source.Kind{Type: &ocsv1.StorageClusterInitialization{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &ocsv1.StorageCluster{},
	})
	if err != nil {
		return err
	}

	err = c.Watch(&source.Kind{Type: &cephv1.CephCluster{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &ocsv1.StorageCluster{},
	})
	if err != nil {
		return err
	}

	return nil
}

var _ reconcile.Reconciler = &ReconcileStorageCluster{}

// ReconcileStorageCluster reconciles a StorageCluster object
type ReconcileStorageCluster struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client     client.Client
	scheme     *runtime.Scheme
	reqLogger  logr.Logger
	conditions []conditionsv1.Condition
	phase      string
	cephImage  string
}
