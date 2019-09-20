package storageclusterinitialization

import (
	"context"
	"fmt"

	"github.com/go-logr/logr"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/operator-framework/operator-sdk/pkg/k8sutil"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
	appsv1 "k8s.io/api/apps/v1"
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

var nodeAffinityKey = "cluster.ocs.openshift.io/openshift-storage"
var nodeTolerationKey = "node.ocs.openshift.io/storage"

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

	if instance.Status.CephToolboxCreated != true {
		// we only create the data once and then allow changes or even deletion
		err = r.ensureToolboxDeployment(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err

		}

		instance.Status.CephToolboxCreated = true
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

	if instance.Status.NoobaaServiceMonitorCreated != true {
		err = r.ensureNoobaaServiceMonitor(instance, reqLogger)
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			instance.Status.Phase = statusutil.PhaseError
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Error(uErr, "Failed to update conditions")
			}
			return reconcile.Result{}, err

		}

		instance.Status.NoobaaServiceMonitorCreated = true
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
				"pool":      generateNameForCephBlockPool(initData),
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
				MetadataPool: cephv1.PoolSpec{
					FailureDomain: "host",
					Replicated: cephv1.ReplicatedSpec{
						Size: 3,
					},
				},
				Gateway: cephv1.GatewaySpec{
					Port:      80,
					Instances: 1,
					Placement: rook.Placement{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											corev1.NodeSelectorRequirement{
												Key:      nodeAffinityKey,
												Operator: corev1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
						Tolerations: []corev1.Toleration{
							corev1.Toleration{
								Key:      nodeTolerationKey,
								Operator: corev1.TolerationOpEqual,
								Value:    "true",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											metav1.LabelSelectorRequirement{
												Key:      "app",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"rook-ceph-rgw"},
											},
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
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
						FailureDomain: "host",
					},
				},
				MetadataServer: cephv1.MetadataServerSpec{
					ActiveCount:   1,
					ActiveStandby: true,
					Placement: rook.Placement{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									corev1.NodeSelectorTerm{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											corev1.NodeSelectorRequirement{
												Key:      nodeAffinityKey,
												Operator: corev1.NodeSelectorOpExists,
											},
										},
									},
								},
							},
						},
						Tolerations: []corev1.Toleration{
							corev1.Toleration{
								Key:      nodeTolerationKey,
								Operator: corev1.TolerationOpEqual,
								Value:    "true",
								Effect:   corev1.TaintEffectNoSchedule,
							},
						},
						PodAntiAffinity: &corev1.PodAntiAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: []corev1.PodAffinityTerm{
								corev1.PodAffinityTerm{
									LabelSelector: &metav1.LabelSelector{
										MatchExpressions: []metav1.LabelSelectorRequirement{
											metav1.LabelSelectorRequirement{
												Key:      "app",
												Operator: metav1.LabelSelectorOpIn,
												Values:   []string{"rook-ceph-mds"},
											},
										},
									},
									TopologyKey: "kubernetes.io/hostname",
								},
							},
						},
					},
				},
			},
		},
	}
	return ret, nil
}

// ensureToolboxDeployment ensures that ceph toolbox exist in the desired
// state.
func (r *ReconcileStorageClusterInitialization) ensureToolboxDeployment(initialData *ocsv1.StorageClusterInitialization, reqLogger logr.Logger) error {
	toolboxInstances, err := r.newToolboxDeploymentInstance(initialData)
	if err != nil {
		return err
	}
	for _, toolboxInstance := range toolboxInstances {
		err := controllerutil.SetControllerReference(initialData, &toolboxInstance, r.scheme)
		if err != nil {
			return err
		}
		existing := appsv1.Deployment{}
		err = r.client.Get(context.TODO(), types.NamespacedName{Name: toolboxInstance.Name, Namespace: toolboxInstance.Namespace}, &existing)

		switch {
		case err == nil:
			reqLogger.Info(fmt.Sprintf("Restoring original toolboxInstance %s", toolboxInstance.Name))
			toolboxInstance.DeepCopyInto(&existing)
			err = r.client.Update(context.TODO(), &existing)
			if err != nil {
				return err
			}
		case errors.IsNotFound(err):
			reqLogger.Info(fmt.Sprintf("Creating toolboxInstance %s", toolboxInstance.Name))
			err = r.client.Create(context.TODO(), &toolboxInstance)
			if err != nil {
				return err
			}
		}
	}
	return err
}

// newToolboxDeploymentInstance returns the toolboxDeployment instances that should be created
// on first run.
func (r *ReconcileStorageClusterInitialization) newToolboxDeploymentInstance(initData *ocsv1.StorageClusterInitialization) ([]appsv1.Deployment, error) {
	privilegedContainer := true
	ret := []appsv1.Deployment{
		appsv1.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "rook-ceph-tools",
				Namespace: initData.Namespace,
			},
			Spec: appsv1.DeploymentSpec{
				Selector: &metav1.LabelSelector{
					MatchLabels: map[string]string{
						"app": "rook-ceph-tools",
					},
				},
				Template: corev1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "rook-ceph-tools",
						},
					},
					Spec: corev1.PodSpec{
						DNSPolicy: corev1.DNSClusterFirstWithHostNet,
						Containers: []corev1.Container{
							corev1.Container{
								Name:    "rook-ceph-tools",
								Image:   "rook/ceph:master",
								Command: []string{"/tini"},
								Args:    []string{"-g", "--", "/usr/local/bin/toolbox.sh"},
								Env: []corev1.EnvVar{
									corev1.EnvVar{
										Name: "ROOK_ADMIN_SECRET",
										ValueFrom: &corev1.EnvVarSource{
											SecretKeyRef: &corev1.SecretKeySelector{
												LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
												Key:                  "admin-secret",
											},
										},
									},
								},
								SecurityContext: &corev1.SecurityContext{
									Privileged: &privilegedContainer,
								},
								VolumeMounts: []corev1.VolumeMount{
									corev1.VolumeMount{Name: "dev", MountPath: "/dev"},
									corev1.VolumeMount{Name: "sysbus", MountPath: "/sys/bus"},
									corev1.VolumeMount{Name: "libmodules", MountPath: "/lib/modules"},
									corev1.VolumeMount{Name: "mon-endpoint-volume", MountPath: "/etc/rook"},
								},
							},
						},
						HostNetwork: false,
						Volumes: []corev1.Volume{
							corev1.Volume{Name: "dev", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/dev"}}},
							corev1.Volume{Name: "sysbus", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/sys/bus"}}},
							corev1.Volume{Name: "libmodules", VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{Path: "/lib/modules"}}},
							corev1.Volume{Name: "mon-endpoint-volume", VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
									Items: []corev1.KeyToPath{
										corev1.KeyToPath{Key: "data", Path: "mon-endpoints"},
									},
								},
							},
							},
						},
					},
				},
			},
		},
	}
	return ret, nil
}
