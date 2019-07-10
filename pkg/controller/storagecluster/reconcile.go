package storagecluster

import (
	"context"
	"reflect"

	"github.com/go-logr/logr"
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
	reqLogger := r.reqLogger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling StorageCluster")

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

	for _, f := range []func(*ocsv1alpha1.StorageCluster, logr.Logger) error{
		// Add support for additional resources here
		r.ensureCephCluster,
	} {
		err = f(instance, reqLogger)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// ensureCephCluster ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (r *ReconcileStorageCluster) ensureCephCluster(sc *ocsv1alpha1.StorageCluster, reqLogger logr.Logger) error {
	// Define a new CephCluster object
	cephCluster := newCephCluster(sc)

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.scheme); err != nil {
		return err
	}

	// Check if this CephCluster already exists
	found := &rookCephv1.CephCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)

	if err != nil && errors.IsNotFound(err) {
		reqLogger.Info("Creating CephCluster")
		err = r.client.Create(context.TODO(), cephCluster)

	} else if err == nil {
		// Update the CephCluster if it is not in the desired state
		if !reflect.DeepEqual(cephCluster.Spec, found.Spec) {
			reqLogger.Info("Updating spec for CephCluster")
			found.Spec = cephCluster.Spec
			err = r.client.Update(context.TODO(), found)
		}
	}
	return err
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
			Monitoring: rookCephv1.MonitoringSpec{
				Enabled:        true,
				RulesNamespace: "openshift-storage",
			},
			Storage: rook.StorageScopeSpec{
				StorageClassDeviceSets: newStorageClassDeviceSets(sc.StorageDeviceSet),
				// XXX: Depending on how rook-ceph is going to use
				// StorageClassDeviceSets we would be setting other required parameters
				// for CephCluster.Storage
			},
		},
	}
}

func newStorageClassDeviceSets(dss []ocsv1alpha1.StorageDeviceSet) []rookCephv1.StorageClassDeviceSet {
	var scds []rookCephv1.StorageClassDeviceSet

	for _, ds := range dss {
		scds = append(scds, newStorageClassDeviceSet(&ds))
	}

	return scds
}

func newStorageClassDeviceSet(ds *ocsv1alpha1.StorageDeviceSet) rookCephv1.StorageClassDeviceSet {
	return rookCephv1.StorageClassDeviceSet{
		// XXX: We should possibly be generating and using a unique name here
		Name:      ds.Name,
		Count:     ds.Count,
		Resources: ds.Resources,
		Placement: ds.Placement,
		// XXX: We should most likely be setting rook-ceph specific config options
		// here, and not rely on user provided config. Just copying user provided config for now.
		Config:               ds.Config,
		VolumeClaimTemplates: ds.VolumeClaimTemplates,
	}
}
