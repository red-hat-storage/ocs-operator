package storagecluster

import (
	"context"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	ocsv1alpha1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1alpha1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
)

// Reconcile reads that state of the cluster for a StorageCluster object and makes changes based on the state read
// and what is in the StorageCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileStorageCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	r.reqLogger = log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.reqLogger.Info("Reconciling StorageCluster")

	// Fetch the StorageCluster instance
	instance := &ocsv1alpha1.StorageCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}
	// Define a new CephCluster object
	cephCluster := newCephCluster(instance)

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(instance, cephCluster, r.scheme); err != nil {
		return reconcile.Result{}, err
	}

	// Check if this CephCluster already exists
	found := &rookCephv1.CephCluster{}
	err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)

	if err != nil && errors.IsNotFound(err) {
		// Create the CephCluster if it doesn't exist
		err = r.createCephCluster(cephCluster)

	} else if err == nil {
		// Update the CephCluster if it exists
		err = r.updateCephCluster(found)

	}

	return reconcile.Result{}, err
}

func (r *ReconcileStorageCluster) updateCephCluster(cephCluster *rookCephv1.CephCluster) error {
	r.reqLogger.Info("Updating the existing CephCluster", "CephCluster.Namespace", cephCluster.Namespace, "CephCluster.Name", cephCluster.Name)
	// TODO: handle updates here by modifying the object here
	return r.client.Update(context.TODO(), cephCluster)
}

func (r *ReconcileStorageCluster) createCephCluster(cephCluster *rookCephv1.CephCluster) error {
	r.reqLogger.Info("Creating the CephCluster", "CephCluster.Namespace", cephCluster.Namespace, "CephCluster.Name", cephCluster.Name)
	return r.client.Create(context.TODO(), cephCluster)
}

// newCephCluster returns a Cephcluster object that doesn't point at any backing storage.
func newCephCluster(sc *ocsv1alpha1.StorageCluster) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}
	return &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sc.Name,
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: rookCephv1.ClusterSpec{
			CephVersion: rookCephv1.CephVersionSpec{
				Image:            "ceph/ceph:v14.2.1-20190430",
				AllowUnsupported: false,
			},
			Mon: rookCephv1.MonSpec{
				Count:                3,
				AllowMultiplePerNode: false,
			},
			DataDirHostPath: "/var/lib/rook",
			RBDMirroring: rookCephv1.RBDMirroringSpec{
				Workers: 0,
			},
			Network: rook.NetworkSpec{
				HostNetwork: false,
			},
		},
	}
}
