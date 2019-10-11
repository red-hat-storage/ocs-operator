package storageclusterinitialization

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_storageclusterinitialization")

// watchNamespace is the namespace the operator is watching.
var watchNamespace string

const wrongNamespacedName = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."

// Add creates a new StorageClusterInitialization Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileStorageClusterInitialization{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// set the watchNamespace so we know where to create the StorageClusterInitialization resource
	ns, err := k8sutil.GetWatchNamespace()
	if err != nil {
		return err
	}
	watchNamespace = ns

	// Create a new controller
	c, err := controller.New("storageclusterinitialization-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource StorageClusterInitialization
	return c.Watch(&source.Kind{Type: &ocsv1.StorageClusterInitialization{}}, &handler.EnqueueRequestForObject{})
}

// blank assignment to verify that ReconcileStorageClusterInitialization implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileStorageClusterInitialization{}

// ReconcileStorageClusterInitialization reconciles a StorageClusterInitialization object
type ReconcileStorageClusterInitialization struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a StorageClusterInitialization object and makes changes based on the state read
// and what is in the StorageClusterInitialization.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileStorageClusterInitialization) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling StorageClusterInitialization")

	instance := &ocsv1.StorageClusterInitialization{}
	if watchNamespace != request.Namespace {
		// Ignoring this resource because it has the wrong namespace
		reqLogger.Info(wrongNamespacedName)
		err := r.client.Get(context.TODO(), request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}
		instance.Status.ErrorMessage = wrongNamespacedName

		instance.Status.Phase = statusutil.PhaseIgnored
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update ignored resource")
		}
		return reconcile.Result{}, err
	}

	// Fetch the StorageClusterInitialization instance
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("No StorageClusterInitialization resource")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing StorageClusterInitialization resource"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)

		instance.Status.Phase = statusutil.PhaseProgressing
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "Failed to add conditions to status")
			return reconcile.Result{}, err
		}
	}

	if instance.Status.StorageClassesCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureStorageClasses(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err
		}

		instance.Status.StorageClassesCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if instance.Status.CephObjectStoresCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureCephObjectStores(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err
		}

		instance.Status.CephObjectStoresCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if instance.Status.CephObjectStoreUsersCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureCephObjectStoreUsers(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err
		}

		instance.Status.CephObjectStoreUsersCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if instance.Status.CephBlockPoolsCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureCephBlockPools(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err
		}

		instance.Status.CephBlockPoolsCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if instance.Status.CephFilesystemsCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureCephFilesystems(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err

		}

		instance.Status.CephFilesystemsCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	if instance.Status.NoobaaSystemCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureNoobaaSystem(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err

		}

		instance.Status.NoobaaSystemCreated = true
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	reason := ocsv1.ReconcileCompleted
	message := ocsv1.ReconcileCompletedMessage
	statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)
	instance.Status.StorageClassesCreated = true
	instance.Status.Phase = statusutil.PhaseReady
	err = r.client.Status().Update(context.TODO(), instance)

	return reconcile.Result{}, err
}

// ensureStorageClasses ensures that StorageClass resources exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureStorageClasses(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	scs, err := r.newStorageClasses(initialData)
	if err != nil {
		return err
	}
	for _, sc := range scs {
		err := controllerutil.SetControllerReference(initialData, &sc, r.scheme)
		if err != nil {
			return err
		}
		existing := storagev1.StorageClass{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original StorageClass %s", sc.Name))
			sc.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), &sc)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating StorageClass %s", sc.Name))
			err = r.client.Create(context.TODO(), &sc)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newStorageClasses returns the StorageClass instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newStorageClasses(initData *ocsv1.StorageClusterInitialization) ([]storagev1.StorageClass, error) {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	ret := []storagev1.StorageClass{
		storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephFilesystemSC(initData),
			},
			Provisioner:   fmt.Sprintf("%s.cephfs.csi.ceph.com", initData.Namespace),
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			Parameters: map[string]string{
				"clusterID": initData.Namespace,
				"fsName":    fmt.Sprintf("%s-cephfilesystem", initData.Name),
				"csi.storage.k8s.io/provisioner-secret-name":      "rook-ceph-csi",
				"csi.storage.k8s.io/provisioner-secret-namespace": initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":       "rook-ceph-csi",
				"csi.storage.k8s.io/node-stage-secret-namespace":  initData.Namespace,
			},
		},
		storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForCephBlockPoolSC(initData),
			},
			Provisioner:   fmt.Sprintf("%s.rbd.csi.ceph.com", initData.Namespace),
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			Parameters: map[string]string{
				"clusterID":                 initData.Namespace,
				"pool":                      generateNameForCephBlockPool(initData),
				"imageFeatures":             "layering",
				"csi.storage.k8s.io/fstype": "ext4",
				"imageFormat":               "2",
				"csi.storage.k8s.io/provisioner-secret-name":      "rook-ceph-csi",
				"csi.storage.k8s.io/provisioner-secret-namespace": initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":       "rook-ceph-csi",
				"csi.storage.k8s.io/node-stage-secret-namespace":  initData.Namespace,
			},
		},
	}
	return ret, nil
}

// ensureCephObjectStores ensures that CephObjectStore resources exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureCephObjectStores(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	cephObjectStores, err := r.newCephObjectStoreInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephObjectStore := range cephObjectStores {
		err := controllerutil.SetControllerReference(initialData, &cephObjectStore, r.scheme)
		if err != nil {
			return err
		}
		existing := cephv1.CephObjectStore{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStore %s", cephObjectStore.Name))
			cephObjectStore.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), &cephObjectStore)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating CephObjectStore %s", cephObjectStore.Name))
			err = r.client.Create(context.TODO(), &cephObjectStore)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newCephObjectStoreInstances returns the cephObjectStore instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newCephObjectStoreInstances(initData *ocsv1.StorageClusterInitialization) ([]cephv1.CephObjectStore, error) {
	ret := []cephv1.CephObjectStore{
		cephv1.CephObjectStore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				DataPool: cephv1.PoolSpec{
					FailureDomain: initData.Spec.FailureDomain,
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				MetadataPool: cephv1.PoolSpec{
					FailureDomain: initData.Spec.FailureDomain,
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				Gateway: cephv1.GatewaySpec{
					Port:      80,
					Instances: 1,
					Placement: defaults.DaemonPlacements["rgw"],
					Resources: defaults.GetDaemonResources("rgw", initData.Spec.Resources),
				},
			},
		},
	}
	return ret, nil
}

// ensureCephBlockPools ensures that cephBlockPool resources exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureCephBlockPools(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	cephBlockPools, err := r.newCephBlockPoolInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephBlockPool := range cephBlockPools {
		err := controllerutil.SetControllerReference(initialData, &cephBlockPool, r.scheme)
		if err != nil {
			return err
		}
		existing := cephv1.CephBlockPool{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: cephBlockPool.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephBlockPool %s", cephBlockPool.Name))
			cephBlockPool.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), &cephBlockPool)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephBlockPool %s", cephBlockPool.Name))
			err = r.client.Create(context.TODO(), &cephBlockPool)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newCephBlockPoolInstances returns the cephBlockPool instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newCephBlockPoolInstances(initData *ocsv1.StorageClusterInitialization) ([]cephv1.CephBlockPool, error) {
	ret := []cephv1.CephBlockPool{
		cephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephBlockPool(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.PoolSpec{
				FailureDomain: initData.Spec.FailureDomain,
				Replicated: cephv1.ReplicatedSpec{
					Size: 3,
				},
			},
		},
	}
	return ret, nil
}

// ensureCephObjectStoreUsers ensures that cephObjectStoreUser resources exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureCephObjectStoreUsers(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		err := controllerutil.SetControllerReference(initialData, &cephObjectStoreUser, r.scheme)
		if err != nil {
			return err
		}
		existing := cephv1.CephObjectStoreUser{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: cephObjectStoreUser.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStoreUser %s", cephObjectStoreUser.Name))
			cephObjectStoreUser.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), &cephObjectStoreUser)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephObjectStoreUser %s", cephObjectStoreUser.Name))
			err = r.client.Create(context.TODO(), &cephObjectStoreUser)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newCephObjectStoreUserInstances returns the cephObjectStoreUser instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newCephObjectStoreUserInstances(initData *ocsv1.StorageClusterInitialization) ([]cephv1.CephObjectStoreUser, error) {
	ret := []cephv1.CephObjectStoreUser{
		cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephObjectStoreUser(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreUserSpec{
				DisplayName: initData.Name,
				Store:       initData.Name,
			},
		},
	}
	return ret, nil
}

// ensureCephFilesystems ensures that cephFilesystem resources exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureCephFilesystems(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	cephFilesystems, err := r.newCephFilesystemInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephFilesystem := range cephFilesystems {
		err := controllerutil.SetControllerReference(initialData, &cephFilesystem, r.scheme)
		if err != nil {
			return err
		}
		existing := cephv1.CephFilesystem{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: cephFilesystem.Namespace}, &existing)
		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephFilesystem %s", cephFilesystem.Name))
			cephFilesystem.ObjectMeta = existing.ObjectMeta
			err = r.client.Update(context.TODO(), &cephFilesystem)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephFilesystem %s", cephFilesystem.Name))
			err = r.client.Create(context.TODO(), &cephFilesystem)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newCephFilesystemInstances returns the cephFilesystem instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newCephFilesystemInstances(initData *ocsv1.StorageClusterInitialization) ([]cephv1.CephFilesystem, error) {
	ret := []cephv1.CephFilesystem{
		cephv1.CephFilesystem{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForCephFilesystem(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.FilesystemSpec{
				MetadataPool: cephv1.PoolSpec{
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				DataPools: []cephv1.PoolSpec{
					cephv1.PoolSpec{
						Replicated: cephv1.ReplicatedSpec{
							Size: 3,
						},
						FailureDomain: initData.Spec.FailureDomain,
					},
				},
				MetadataServer: cephv1.MetadataServerSpec{
					ActiveCount:   1,
					ActiveStandby: true,
					Placement:     defaults.DaemonPlacements["mds"],
					Resources:     defaults.GetDaemonResources("mds", initData.Spec.Resources),
				},
			},
		},
	}
	return ret, nil
}
