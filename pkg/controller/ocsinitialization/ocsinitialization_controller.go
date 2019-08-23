package ocsinitialization

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1alpha1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1alpha1"
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
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	logf "sigs.k8s.io/controller-runtime/pkg/runtime/log"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

var log = logf.Log.WithName("controller_ocsinitialization")

// watchNamespace is the namespace the operator is watching.
var watchNamespace string

const wrongNamespacedName = "Ignoring this resource. Only one should exist, and this one has the wrong name and/or namespace."

// InitNamespacedName returns a NamespacedName for the singleton instance that
// should exist.
func InitNamespacedName() types.NamespacedName {
	return types.NamespacedName{
		Name:      "ocsinit",
		Namespace: watchNamespace,
	}
}

// Add creates a new OCSInitialization Controller and adds it to the Manager. The Manager will set fields on the Controller
// and Start it when the Manager is Started.
func Add(mgr manager.Manager) error {
	return add(mgr, newReconciler(mgr))
}

// newReconciler returns a new reconcile.Reconciler
func newReconciler(mgr manager.Manager) reconcile.Reconciler {
	return &ReconcileOCSInitialization{client: mgr.GetClient(), scheme: mgr.GetScheme()}
}

// add adds a new Controller to mgr with r as the reconcile.Reconciler
func add(mgr manager.Manager, r reconcile.Reconciler) error {
	// set the watchNamespace so we know where to create the OCSInitialization resource
	ns, err := k8sutil.GetWatchNamespace()
	if err != nil {
		return err
	}
	watchNamespace = ns

	// Create a new controller
	c, err := controller.New("ocsinitialization-controller", mgr, controller.Options{Reconciler: r})
	if err != nil {
		return err
	}

	// Watch for changes to primary resource OCSInitialization
	return c.Watch(&source.Kind{Type: &ocsv1alpha1.OCSInitialization{}}, &handler.EnqueueRequestForObject{})
}

// blank assignment to verify that ReconcileOCSInitialization implements reconcile.Reconciler
var _ reconcile.Reconciler = &ReconcileOCSInitialization{}

// ReconcileOCSInitialization reconciles a OCSInitialization object
type ReconcileOCSInitialization struct {
	// This client, initialized using mgr.Client() above, is a split client
	// that reads objects from the cache and writes to the apiserver
	client client.Client
	scheme *runtime.Scheme
}

// Reconcile reads that state of the cluster for a OCSInitialization object and makes changes based on the state read
// and what is in the OCSInitialization.Spec
// TODO(user): Modify this Reconcile function to implement your Controller logic.  This example creates
// a Pod as an example
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileOCSInitialization) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling OCSInitialization")

	initNamespacedName := InitNamespacedName()
	instance := &ocsv1alpha1.OCSInitialization{}
	if initNamespacedName.Name != request.Name || initNamespacedName.Namespace != request.Namespace {
		// Ignoring this resource because it has the wrong name or namespace
		reqLogger.Info(wrongNamespacedName)
		err := r.client.Get(context.TODO(), request.NamespacedName, instance)
		if err != nil {
			// the resource probably got deleted
			if errors.IsNotFound(err) {
				return reconcile.Result{}, nil
			}
			return reconcile.Result{}, err
		}

		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "failed to update ignored resource")
		}
		return reconcile.Result{}, err
	}

	// Fetch the OCSInitialization instance
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Recreating since we depend on this to exist. A user may delete it to
			// induce a reset of all initial data.
			reqLogger.Info("recreating OCSInitialization resource")
			return reconcile.Result{}, r.client.Create(context.TODO(), &ocsv1alpha1.OCSInitialization{
				ObjectMeta: metav1.ObjectMeta{
					Name:      initNamespacedName.Name,
					Namespace: initNamespacedName.Namespace,
				},
			})
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Status.Conditions == nil {
		reason := ocsv1alpha1.ReconcileInit
		message := "Initializing OCSInitialization resource"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)

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
			reason := ocsv1alpha1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

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
			reason := ocsv1alpha1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

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
			reason := ocsv1alpha1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

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
			reason := ocsv1alpha1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

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
			reason := ocsv1alpha1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

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

	reason := ocsv1alpha1.ReconcileCompleted
	message := ocsv1alpha1.ReconcileCompletedMessage
	statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)
	instance.Status.StorageClassesCreated = true
	err = r.client.Status().Update(context.TODO(), instance)

	return reconcile.Result{}, err
}

// ensureStorageClasses ensures that StorageClass resources exist in the desired
// state.
func (r *ReconcileOCSInitialization) ensureStorageClasses(initialData *ocsv1alpha1.OCSInitialization, reqLogger logr.Logger) error {
	scs, err := r.newStorageClasses(initialData)
	if err != nil {
		return err
	}
	for _, sc := range scs {
		existing := storagev1.StorageClass{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: sc.Name, Namespace: sc.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original StorageClass %s", sc.Name))
			sc.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating StorageClass %s", sc.Name))
			err = r.client.Create(context.TODO(), &sc)
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// newStorageClasses returns the StorageClass instances that should be created
// on first run.
func (r *ReconcileOCSInitialization) newStorageClasses(initData *ocsv1alpha1.OCSInitialization) ([]storagev1.StorageClass, error) {
	persistentVolumeReclaimDelete := corev1.PersistentVolumeReclaimDelete
	ret := []storagev1.StorageClass{
		storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForOCSFilesystemSC(initData),
			},
			Provisioner:   "rook-ceph.cephfs.csi.ceph.com",
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			Parameters: map[string]string{
				"clusterID": initData.Namespace,
				"fsName":    initData.Name + "fs",
				"pool":      generateNameForOCSBlockPool(initData),
				"csi.storage.k8s.io/provisioner-secret-name":      "rook-ceph-csi",
				"csi.storage.k8s.io/provisioner-secret-namespace": initData.Namespace,
				"csi.storage.k8s.io/node-stage-secret-name":       "rook-ceph-csi",
				"csi.storage.k8s.io/node-stage-secret-namespace":  initData.Namespace,
			},
		},
		storagev1.StorageClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: generateNameForOCSBlockPoolSC(initData),
			},
			Provisioner:   "rook-ceph.rbd.csi.ceph.com",
			ReclaimPolicy: &persistentVolumeReclaimDelete,
			Parameters: map[string]string{
				"clusterID":                 initData.Namespace,
				"pool":                      generateNameForOCSBlockPool(initData),
				"imageFeatures":             "layering",
				"csi.storage.k8s.io/fstype": "xfs",
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
func (r *ReconcileOCSInitialization) ensureCephObjectStores(initialData *ocsv1alpha1.OCSInitialization, reqLogger logr.Logger) error {
	cephObjectStores, err := r.newCephObjectStoreInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephObjectStore := range cephObjectStores {
		existing := cephv1.CephObjectStore{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStore.Name, Namespace: cephObjectStore.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStore %s", cephObjectStore.Name))
			cephObjectStore.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating CephObjectStore %s", cephObjectStore.Name))
			err = r.client.Create(context.TODO(), &cephObjectStore)
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// newCephObjectStoreInstances returns the cephObjectStore instances that should be created
// on first run.
func (r *ReconcileOCSInitialization) newCephObjectStoreInstances(initData *ocsv1alpha1.OCSInitialization) ([]cephv1.CephObjectStore, error) {
	ret := []cephv1.CephObjectStore{
		cephv1.CephObjectStore{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForOCSObjectStore(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.ObjectStoreSpec{
				MetadataPool: cephv1.PoolSpec{
					FailureDomain: "host",
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				Gateway: cephv1.GatewaySpec{
					Port:      80,
					Instances: 1,
				},
			},
		},
	}
	return ret, nil
}

// ensureCephBlockPools ensures that cephBlockPool resources exist in the desired
// state.
func (r *ReconcileOCSInitialization) ensureCephBlockPools(initialData *ocsv1alpha1.OCSInitialization, reqLogger logr.Logger) error {
	cephBlockPools, err := r.newCephBlockPoolInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephBlockPool := range cephBlockPools {
		existing := cephv1.CephBlockPool{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephBlockPool.Name, Namespace: cephBlockPool.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephBlockPool %s", cephBlockPool.Name))
			cephBlockPool.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephBlockPool %s", cephBlockPool.Name))
			err = r.client.Create(context.TODO(), &cephBlockPool)
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// newCephBlockPoolInstances returns the cephBlockPool instances that should be created
// on first run.
func (r *ReconcileOCSInitialization) newCephBlockPoolInstances(initData *ocsv1alpha1.OCSInitialization) ([]cephv1.CephBlockPool, error) {
	ret := []cephv1.CephBlockPool{
		cephv1.CephBlockPool{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForOCSBlockPool(initData),
				Namespace: initData.Namespace,
			},
			Spec: cephv1.PoolSpec{
				FailureDomain: "host",
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
func (r *ReconcileOCSInitialization) ensureCephObjectStoreUsers(initialData *ocsv1alpha1.OCSInitialization, reqLogger logr.Logger) error {
	cephObjectStoreUsers, err := r.newCephObjectStoreUserInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephObjectStoreUser := range cephObjectStoreUsers {
		existing := cephv1.CephObjectStoreUser{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephObjectStoreUser.Name, Namespace: cephObjectStoreUser.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephObjectStoreUser %s", cephObjectStoreUser.Name))
			cephObjectStoreUser.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephObjectStoreUser %s", cephObjectStoreUser.Name))
			err = r.client.Create(context.TODO(), &cephObjectStoreUser)
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// newCephObjectStoreUserInstances returns the cephObjectStoreUser instances that should be created
// on first run.
func (r *ReconcileOCSInitialization) newCephObjectStoreUserInstances(initData *ocsv1alpha1.OCSInitialization) ([]cephv1.CephObjectStoreUser, error) {
	ret := []cephv1.CephObjectStoreUser{
		cephv1.CephObjectStoreUser{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForOCSObjectStoreUser(initData),
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
func (r *ReconcileOCSInitialization) ensureCephFilesystems(initialData *ocsv1alpha1.OCSInitialization, reqLogger logr.Logger) error {
	cephFilesystems, err := r.newCephFilesystemInstances(initialData)
	if err != nil {
		return err
	}
	for _, cephFilesystem := range cephFilesystems {
		existing := cephv1.CephFilesystem{}
		err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephFilesystem.Name, Namespace: cephFilesystem.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original cephFilesystem %s", cephFilesystem.Name))
			cephFilesystem.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating cephFilesystem %s", cephFilesystem.Name))
			err = r.client.Create(context.TODO(), &cephFilesystem)
			if err != nil {
				return err
			}
		default:
			return err
		}
	}
	return nil
}

// newCephFilesystemInstances returns the cephFilesystem instances that should be created
// on first run.
func (r *ReconcileOCSInitialization) newCephFilesystemInstances(initData *ocsv1alpha1.OCSInitialization) ([]cephv1.CephFilesystem, error) {
	ret := []cephv1.CephFilesystem{
		cephv1.CephFilesystem{
			ObjectMeta: metav1.ObjectMeta{
				Name:      generateNameForOCSFilesystem(initData),
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
						FailureDomain: "host",
					},
				},
				MetadataServer: cephv1.MetadataServerSpec{
					ActiveCount:   1,
					ActiveStandby: true,
				},
			},
		},
	}
	return ret, nil
}
