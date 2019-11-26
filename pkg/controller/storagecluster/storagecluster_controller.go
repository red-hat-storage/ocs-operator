package storagecluster

import (
	"fmt"
	"os"

	"github.com/go-logr/logr"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
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

func (r *ReconcileStorageCluster) initializeImageVars() error {
	r.cephImage = os.Getenv("CEPH_IMAGE")
	r.noobaaCoreImage = os.Getenv("NOOBAA_CORE_IMAGE")
	r.noobaaDBImage = os.Getenv("NOOBAA_DB_IMAGE")

	if r.cephImage == "" {
		err := fmt.Errorf("CEPH_IMAGE environment variable not found")
		log.Error(err, "missing required environment variable for ocs initialization")
		return err
	} else if r.noobaaCoreImage == "" {
		err := fmt.Errorf("NOOBAA_CORE_IMAGE environment variable not found")
		log.Error(err, "missing required environment variable for ocs initialization")
		return err
	} else if r.noobaaDBImage == "" {
		err := fmt.Errorf("NOOBAA_DB_IMAGE environment variable not found")
		log.Error(err, "missing required environment variable for ocs initialization")
		return err
	}
	return nil
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {

	r := &ReconcileStorageCluster{
		client:    mgr.GetClient(),
		scheme:    mgr.GetScheme(),
		reqLogger: log,
		platform:  &CloudPlatform{},
	}

	err := r.initializeImageVars()
	if err != nil {
		panic(err)
	}

	return r
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

	err = c.Watch(&source.Kind{Type: &nbv1.NooBaa{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &ocsv1.StorageCluster{},
	})
	if err != nil {
		return err
	}

	pred := predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			// Evaluates to false if the object has been confirmed deleted.
			return !e.DeleteStateUnknown
		},
	}
	err = c.Watch(&source.Kind{Type: &corev1.PersistentVolumeClaim{}}, &handler.EnqueueRequestForOwner{
		IsController: true,
		OwnerType:    &ocsv1.StorageCluster{},
	}, pred)
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
	client          client.Client
	scheme          *runtime.Scheme
	reqLogger       logr.Logger
	conditions      []conditionsv1.Condition
	phase           string
	cephImage       string
	noobaaDBImage   string
	noobaaCoreImage string
	platform        *CloudPlatform
}
