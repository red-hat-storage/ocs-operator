package storagecluster

import (
	"context"
	error1 "errors"
	"fmt"
	"strings"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/operator-framework/operator-lib/conditions"
	corev1 "k8s.io/api/core/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	ctrllog "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileStrategy is a string representing how we want to reconcile
// (or not) a particular resource
type ReconcileStrategy string

// StorageClassProvisionerType is a string representing StorageClass Provisioner. E.g: aws-ebs
type StorageClassProvisionerType string

type resourceManager interface {
	ensureCreated(*StorageClusterReconciler, *ocsv1.StorageCluster) (reconcile.Result, error)
	ensureDeleted(*StorageClusterReconciler, *ocsv1.StorageCluster) (reconcile.Result, error)
}

type ocsJobTemplates struct{}

const (
	// Name of MetadataPVCTemplate
	metadataPVCName = "metadata"
	// Name of WalPVCTemplate
	walPVCName = "wal"

	// ReconcileStrategyUnknown is the same as default
	ReconcileStrategyUnknown ReconcileStrategy = ""
	// ReconcileStrategyInit means reconcile once and ignore if it exists
	ReconcileStrategyInit ReconcileStrategy = "init"
	// ReconcileStrategyIgnore means never reconcile
	ReconcileStrategyIgnore ReconcileStrategy = "ignore"
	// ReconcileStrategyManage means always reconcile
	ReconcileStrategyManage ReconcileStrategy = "manage"
	// ReconcileStrategyStandalone means to renconcile the component exclusively (NooBaa)
	ReconcileStrategyStandalone ReconcileStrategy = "standalone"

	// DeviceTypeSSD represents the DeviceType SSD
	DeviceTypeSSD = "ssd"

	// DeviceTypeHDD represents the DeviceType HDD
	DeviceTypeHDD = "hdd"

	// DeviceTypeNVMe represents the DeviceType NVMe
	DeviceTypeNVMe = "nvme"

	// AzureDisk represents Azure Premium Managed Disks provisioner for StorageClass
	AzureDisk StorageClassProvisionerType = "kubernetes.io/azure-disk"

	// EBS represents AWS EBS provisioner for StorageClass
	EBS StorageClassProvisionerType = "kubernetes.io/aws-ebs"

	VirtualMachineCrdName = "virtualmachines.kubevirt.io"

	StorageClientCrdName = "storageclients.ocs.openshift.io"

	VolumeGroupSnapshotClassCrdName = "volumegroupsnapshotclasses.groupsnapshot.storage.k8s.io"

	OdfVolumeGroupSnapshotClassCrdName = "volumegroupsnapshotclasses.groupsnapshot.storage.openshift.io"

	internalComponentFinalizer = "ocs.openshift.io/internal-component"
)

var storageClusterFinalizer = "storagecluster.ocs.openshift.io"

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclusters;cephblockpools;cephfilesystems;cephnfses;cephobjectstores;cephobjectstoreusers;cephrbdmirrors;cephblockpoolradosnamespaces,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaas,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=watch;create;update;delete;get;list
// +kubebuilder:rbac:groups=core,resources=pods;services;serviceaccounts;endpoints;persistentvolumes;persistentvolumeclaims;events;configmaps;secrets;nodes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=template.openshift.io,resources=templates,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshotclasses,verbs=get;watch;create;update;delete
// +kubebuilder:rbac:groups=groupsnapshot.storage.k8s.io,resources=volumegroupsnapshotclasses,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=groupsnapshot.storage.openshift.io,resources=volumegroupsnapshotclasses,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures;networks,verbs=get;list;watch
// +kubebuilder:rbac:groups=config.openshift.io,resources=clusterversions;networks,verbs=get;list;watch
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=create;delete;list;watch;update
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update
// +kubebuilder:rbac:groups=operators.coreos.com,resources=operatorconditions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=quota.openshift.io,resources=clusterresourcequotas,verbs=get;list;watch;create;update;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs;jobs,verbs=get;list;create;update;watch;delete
// +kubebuilder:rbac:groups=operators.coreos.com,resources=clusterserviceversions,verbs=get;list;watch;update
// +kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterrolebindings;rolebindings;clusterroles;roles,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=objectbucket.io,resources=objectbuckets;objectbucketclaims,verbs=get;list;watch
// +kubebuilder:rbac:groups=authentication.k8s.io,resources=tokenreviews,verbs=create
// +kubebuilder:rbac:groups=authorization.k8s.io,resources=subjectaccessreviews,verbs=create
// +kubebuilder:rbac:groups=security.openshift.io,resources=securitycontextconstraints,resourceNames=hostnetwork-v2,verbs=use
// +kubebuilder:rbac:groups=csiaddons.openshift.io,resources=networkfenceclasses,verbs=get;list;watch;create;update;delete

// Reconcile reads that state of the cluster for a StorageCluster object and makes changes based on the state read
// and what is in the StorageCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *StorageClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	r.ctx = ctrllog.IntoContext(ctx, r.Log)

	for _, crdName := range []string{VirtualMachineCrdName, StorageClientCrdName, VolumeGroupSnapshotClassCrdName, OdfVolumeGroupSnapshotClassCrdName} {
		crd := &metav1.PartialObjectMetadata{}
		crd.SetGroupVersionKind(extv1.SchemeGroupVersion.WithKind("CustomResourceDefinition"))
		crd.Name = crdName
		if err := r.Client.Get(ctx, client.ObjectKeyFromObject(crd), crd); client.IgnoreNotFound(err) != nil {
			r.Log.Error(err, "Failed to get CRD", "CRD", crd.Name)
			return reconcile.Result{}, err
		}
		util.AssertEqual(r.AvailableCrds[crd.Name], crd.UID != "", util.ExitCodeThatShouldRestartTheProcess)
	}

	// Fetch the StorageCluster instance
	sc := &ocsv1.StorageCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, sc); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("No StorageCluster resource.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.Log.Error(err, "Failed to retrieve StorageCluster.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
		return reconcile.Result{}, err
	}

	if err := r.validateStorageClusterSpec(sc); err != nil {
		return reconcile.Result{}, err
	}

	r.IsNoobaaStandalone = sc.Spec.MultiCloudGateway != nil &&
		ReconcileStrategy(sc.Spec.MultiCloudGateway.ReconcileStrategy) == ReconcileStrategyStandalone

	var err error
	r.clusters, err = statusutil.GetClusters(ctx, r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to get clusters")
		return reconcile.Result{}, err
	}
	r.IsMultipleStorageClusters = len(r.clusters.GetStorageClusters()) > 1

	// Reconcile changes to the cluster
	result, reconcileError := r.reconcilePhases(ctx, sc)

	// Ensure that cephtoolbox is deployed as instructed by the user
	err = r.ensureToolsDeployment(sc)
	if err != nil {
		r.Log.Error(err, "Failed to process ceph tools deployment.", "CephToolDeployment", klog.KRef(sc.Namespace, rookCephToolDeploymentName))
		return reconcile.Result{}, err
	}

	// Apply status changes to the storagecluster
	statusError := r.Client.Status().Update(ctx, sc)
	if statusError != nil {
		r.Log.Info("Could not update StorageCluster status.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	}
	return result, nil
}

func (r *StorageClusterReconciler) initializeImagesStatus(sc *ocsv1.StorageCluster) {
	images := &sc.Status.Images
	if images.Ceph == nil {
		images.Ceph = &ocsv1.ComponentImageStatus{}
	}
	images.Ceph.DesiredImage = r.images.Ceph

	if images.NooBaaCore == nil {
		images.NooBaaCore = &ocsv1.ComponentImageStatus{}
	}
	images.NooBaaCore.DesiredImage = r.images.NooBaaCore

	if images.NooBaaDB == nil {
		images.NooBaaDB = &ocsv1.ComponentImageStatus{}
	}
	images.NooBaaDB.DesiredImage = r.images.NooBaaDB
}

// validateStorageClusterSpec must be called before reconciling. Any syntactic and semantic errors in the CR must be caught here.
func (r *StorageClusterReconciler) validateStorageClusterSpec(instance *ocsv1.StorageCluster) error {
	if err := versionCheck(instance, r.Log); err != nil {
		r.Log.Error(err, "Failed to validate StorageCluster version.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		reason := statusutil.EventReasonValidationFailed
		message := err.Error()
		statusutil.SetVersionMismatchCondition(&instance.Status.Conditions, corev1.ConditionTrue, reason, message)
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			r.Log.Error(updateErr, "Failed to update StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			return updateErr
		}
		return err
	}

	reason := statusutil.VersionValidReason
	message := "Version check successful"
	statusutil.SetVersionMismatchCondition(&instance.Status.Conditions, corev1.ConditionFalse, reason, message)

	if !instance.Spec.ExternalStorage.Enable {
		if err := r.validateStorageDeviceSets(instance); err != nil {
			r.Log.Error(err, "Failed to validate StorageDeviceSets.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
			instance.Status.Phase = statusutil.PhaseError
			if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
				r.Log.Error(updateErr, "Failed to update StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
				return updateErr
			}
			return err
		}
	}

	if err := validateArbiterSpec(instance); err != nil {
		r.Log.Error(err, "Failed to validate ArbiterSpec.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			r.Log.Error(updateErr, "Could not update StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			return updateErr
		}
		return err
	}

	if err := validateOverprovisionControlSpec(instance); err != nil {
		r.Log.Error(err, "Failed to validate OverprovisionControlSpec.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			r.Log.Error(updateErr, "Could not update StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			return updateErr
		}
		return err
	}

	if err := validateCustomStorageClassNames(instance); err != nil {
		r.Log.Error(err, "Failed to validate custom StorageClassNames.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			r.Log.Error(updateErr, "Could not update StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			return updateErr
		}
		return err
	}

	return nil
}

func (r *StorageClusterReconciler) reconcilePhases(
	ctx context.Context,
	instance *ocsv1.StorageCluster) (reconcile.Result, error) {

	if instance.Spec.ExternalStorage.Enable {
		r.Log.Info("Reconciling external StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
	} else {
		r.Log.Info("Reconciling StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
	}

	// Initialize the StatusImages section of the storageclsuter CR
	r.initializeImagesStatus(instance)

	// Check for active StorageCluster only if Create request is made
	// and ignore it if there's another active StorageCluster
	// If Update request is made and StorageCluster is PhaseIgnored, no need to
	// proceed further
	if instance.Status.Phase == "" {
		isActive := r.isActiveStorageCluster(instance)
		if !isActive {
			instance.Status.Phase = statusutil.PhaseIgnored
			return reconcile.Result{}, nil
		}
	} else if instance.Status.Phase == statusutil.PhaseIgnored {
		return reconcile.Result{}, nil
	}

	if instance.Status.Phase != statusutil.PhaseReady &&
		instance.Status.Phase != statusutil.PhaseClusterExpanding &&
		instance.Status.Phase != statusutil.PhaseDeleting &&
		instance.Status.Phase != statusutil.PhaseOnboarding &&
		instance.Status.Phase != statusutil.PhaseConnecting {
		instance.Status.Phase = statusutil.PhaseProgressing
	}

	// Add conditions if there are none
	if len(instance.Status.Conditions) == 1 && instance.Status.Conditions[0].Type == ocsv1.ConditionVersionMismatch {
		reason := ocsv1.ReconcileInit
		message := "Initializing StorageCluster"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)
	}

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if instance.GetDeletionTimestamp().IsZero() {
		var updateRequired bool

		// Check if finalizer needs to be added
		if !contains(instance.GetFinalizers(), storageClusterFinalizer) {
			r.Log.Info("Finalizer not found for StorageCluster. Adding finalizer.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			updateRequired = true
		}

		// Check if annotations need to be updated
		if r.checkAndSetUninstallAnnotations(instance) {
			updateRequired = true
		}

		if updateRequired {
			if err := r.Client.Update(r.ctx, instance); err != nil {
				r.Log.Info("Failed to update StorageCluster", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
				return reconcile.Result{}, err
			}
			r.Log.Info("exiting reconcile & requeueing another, immediately after updating storagecluster finalizer or uninstall/cleanup annotations")
			return reconcile.Result{Requeue: true}, nil
		}

	} else {
		// The object is marked for deletion
		instance.Status.Phase = statusutil.PhaseDeleting

		if contains(instance.GetFinalizers(), storageClusterFinalizer) {
			if res, err := r.deleteResources(instance); err != nil {
				r.Log.Info("Uninstall in progress.", "Status", err)
				r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonUninstallPending, err.Error())
				return reconcile.Result{RequeueAfter: time.Second * time.Duration(1)}, nil
			} else if !res.IsZero() {
				// result is not empty
				return res, nil
			}
			r.Log.Info("Removing finalizer from StorageCluster.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
			// Once all finalizers have been removed, the object will be deleted
			instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				r.Log.Info("Failed to remove finalizer from StorageCluster", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))
				return reconcile.Result{}, err
			}
		}
		r.Log.Info("StorageCluster is terminated, skipping reconciliation.", "StorageCluster", klog.KRef(instance.Namespace, instance.Name))

		// mark operator upgradeable if and only if all other storageclusters are ready or current cluster is the last cluster
		if r.clusters.AreOtherStorageClustersReady(instance) {
			returnErr := r.SetOperatorConditions("Skipping StorageCluster reconciliation", "Terminated", metav1.ConditionTrue, nil)
			return reconcile.Result{}, returnErr
		}

		return reconcile.Result{}, nil
	}

	if res, err := r.ownStorageClusterPeersInNamespace(instance); err != nil || !res.IsZero() {
		return reconcile.Result{}, err
	}

	if res, err := r.ownStorageConsumersInNamespace(instance); err != nil || !res.IsZero() {
		return reconcile.Result{}, err
	}

	if instance.Spec.ExternalStorage.Enable {
		if err := r.setExternalOCSResourcesData(instance); err != nil {
			r.Log.Error(err, "Failed to set the externalOCSResources cache")
			return reconcile.Result{}, err
		}
	}

	// in-memory conditions should start off empty. It will only ever hold
	// negative conditions (!Available, Degraded, Progressing)
	r.conditions = nil
	// Start with empty r.phase
	r.phase = ""
	var objs []resourceManager
	if !instance.Spec.ExternalStorage.Enable {
		if !r.IsNoobaaStandalone {
			// list of default ensure functions
			// preserve list order
			objs = []resourceManager{
				&rookCephCsvHostNetwork{},
				&ocsProviderServer{},
				&backingStorageClasses{},
				&ocsTopologyMap{},
				&ocsStorageQuota{},
				&ocsCephConfig{},
				&ocsCephCluster{},
				&storageConsumer{},
				&storageClient{},
				&ocsCephBlockPools{},
				&ocsCephFilesystems{},
				&ocsCephNFS{},
				&ocsCephNFSService{},
				&ocsCephObjectStores{},
				&ocsCephObjectStoreUsers{},
				&ocsCephRGWRoutes{},
				&obcStorageClasses{},
				&ocsNoobaaSystem{},
				&ocsJobTemplates{},
				&ocsCephRbdMirrors{},
				&odfInfoConfig{},
			}
		} else {
			// noobaa-only ensure functions
			objs = []resourceManager{
				&ocsNoobaaSystem{},
			}
		}

	} else {
		// for external cluster, we have a different set of ensure functions
		// preserve list order
		objs = []resourceManager{
			&ocsCephCluster{},
			&ocsExternalResources{},
			&ocsStorageQuota{},
			&ocsSnapshotClass{},
			&ocsGroupSnapshotClass{},
			&ocsOdfGroupSnapshotClass{},
			&ocsNoobaaSystem{},
			&odfInfoConfig{},
		}
	}

	for _, obj := range objs {
		returnRes, returnErr := obj.ensureCreated(r, instance)
		if r.phase == statusutil.PhaseClusterExpanding {
			message := "StorageCluster is expanding"
			reason := "Expanding"
			returnErr = r.SetOperatorConditions(message, reason, metav1.ConditionFalse, returnErr)
			instance.Status.Phase = statusutil.PhaseClusterExpanding
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
				Type:    conditionsv1.ConditionUpgradeable,
				Status:  corev1.ConditionFalse,
				Reason:  reason,
				Message: message,
			})
		} else if instance.Status.Phase != statusutil.PhaseReady &&
			instance.Status.Phase != statusutil.PhaseOnboarding &&
			instance.Status.Phase != statusutil.PhaseConnecting {
			instance.Status.Phase = statusutil.PhaseProgressing
		}
		if returnErr != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", returnErr)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)

			if instance.Status.Phase != statusutil.PhaseOnboarding {
				// if the error was due to skipped storage classes
				// instead of error phase we will set it to progressing
				if strings.Contains(returnErr.Error(), storageClassSkippedError) {
					instance.Status.Phase = statusutil.PhaseProgressing
					return reconcile.Result{Requeue: true}, nil
				}
				instance.Status.Phase = statusutil.PhaseError
			}

			// don't want to overwrite the actual reconcile failure
			return reconcile.Result{}, returnErr
		} else if !returnRes.IsZero() {
			// result is not empty
			return returnRes, nil
		}
	}
	// Process resource profiles only if the cluster is not external or provider mode or noobaa standalone, and if the resource profile has changed
	if !(instance.Spec.ExternalStorage.Enable || r.IsNoobaaStandalone) &&
		(instance.Spec.ResourceProfile != instance.Status.LastAppliedResourceProfile) {
		err := r.ensureResourceProfileChangeApplied(instance)
		if err != nil {
			if err == errResourceProfileChangeApplying {
				reason := ocsv1.ReconcileFailed
				message := err.Error()
				statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)
				instance.Status.Phase = statusutil.PhaseProgressing
				return reconcile.Result{Requeue: true}, nil
			} else if err == errResourceProfileChangeFailed {
				reason := ocsv1.ReconcileFailed
				message := err.Error()
				statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)
				instance.Status.Phase = statusutil.PhaseError
			}
			return reconcile.Result{}, err
		}
	}
	// All component operators are in a happy state.
	if r.conditions == nil {
		r.Log.Info("No component operator reported negatively.")
		reason := ocsv1.ReconcileCompleted
		message := ocsv1.ReconcileCompletedMessage
		statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)

		if instance.Spec.ExternalStorage.Enable {
			statusutil.RemoveExternalCephClusterNegativeConditions(&instance.Status.Conditions)
		}

		// If no operator whose conditions we are watching reports an error, then it is safe
		// to set upgradeable to true.
		if instance.Status.Phase != statusutil.PhaseClusterExpanding {
			instance.Status.Phase = statusutil.PhaseReady

			var returnErr error
			var notUpgradeableReasons, notUpgradeableMessages []string
			// mark operator upgradeable if and only if all storageclusters are ready
			if !r.clusters.AreOtherStorageClustersReady(instance) {
				notUpgradeableReasons = append(notUpgradeableReasons, "NotReady")
				notUpgradeableMessages = append(notUpgradeableMessages, "StorageCluster is not ready")
			}

			if count, err := getUnsupportedClientsCount(r, instance.Namespace); err != nil {
				notUpgradeableReasons = append(notUpgradeableReasons, "ODFClients")
				notUpgradeableMessages = append(notUpgradeableMessages, "Unable to determine status of connected ODF Clients")
			} else if count != 0 {
				notUpgradeableReasons = append(notUpgradeableReasons, "ODFClients")
				notUpgradeableMessages = append(notUpgradeableMessages, fmt.Sprintf("%d connected ODF Client Operators are not up to date", count))
			}

			if len(notUpgradeableMessages) > 0 {
				// we are not upgradeable
				returnErr = r.SetOperatorConditions(
					strings.Join(notUpgradeableMessages, ";"), strings.Join(notUpgradeableReasons, ";"),
					metav1.ConditionFalse, nil)
			} else {
				// we are upgradeable
				returnErr = r.SetOperatorConditions(message, reason, metav1.ConditionTrue, nil)
			}
			if returnErr != nil {
				return reconcile.Result{}, returnErr
			}
		}
	} else {
		// If any component operator reports negatively we want to write that to
		// the instance while preserving it's lastTransitionTime.
		// For example, consider the resource has the Available condition
		// type with type "False". When reconciling the resource we would
		// add it to the in-memory representation of OCS's conditions (r.conditions)
		// and here we are simply writing it back to the server.
		// One shortcoming is that only one failure of a particular condition can be
		// captured at one time (ie. if resource1 and resource2 are both reporting !Available,
		// you will only see resource2q as it updates last).
		for _, condition := range r.conditions {
			conditionsv1.SetStatusCondition(&instance.Status.Conditions, condition)
		}
		reason := ocsv1.ReconcileCompleted
		message := ocsv1.ReconcileCompletedMessage
		conditionsv1.SetStatusCondition(&instance.Status.Conditions, conditionsv1.Condition{
			Type:    ocsv1.ConditionReconcileComplete,
			Status:  corev1.ConditionTrue,
			Reason:  reason,
			Message: message,
		})

		// If for any reason we marked ourselves !upgradeable...then unset readiness
		if conditionsv1.IsStatusConditionFalse(instance.Status.Conditions, conditionsv1.ConditionUpgradeable) {
			returnErr := r.SetOperatorConditions("StorageCluster is not ready.", "NotReady", metav1.ConditionFalse, nil)
			if returnErr != nil {
				return reconcile.Result{}, returnErr
			}
		}
		if instance.Status.Phase != statusutil.PhaseClusterExpanding &&
			!instance.Spec.ExternalStorage.Enable {
			if conditionsv1.IsStatusConditionTrue(instance.Status.Conditions, conditionsv1.ConditionProgressing) {
				instance.Status.Phase = statusutil.PhaseProgressing
			} else if conditionsv1.IsStatusConditionFalse(instance.Status.Conditions, conditionsv1.ConditionUpgradeable) {
				instance.Status.Phase = statusutil.PhaseNotReady
			} else {
				instance.Status.Phase = statusutil.PhaseError
			}
		}
	}

	// enable metrics exporter at the end of reconcile
	// this allows storagecluster to be instantiated before
	// scraping metrics
	if instance.Spec.Monitoring == nil || ReconcileStrategy(instance.Spec.Monitoring.ReconcileStrategy) != ReconcileStrategyIgnore {
		if instance.Spec.Monitoring == nil {
			instance.Spec.Monitoring = &ocsv1.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyUnknown),
			}
		}
		if err := r.enableMetricsExporter(ctx, instance); err != nil {
			r.Log.Error(err, "Failed to reconcile metrics exporter.")
			return reconcile.Result{}, err
		}

		if err := r.enablePrometheusRules(ctx, instance); err != nil {
			r.Log.Error(err, "Failed to reconcile prometheus rules.")
			return reconcile.Result{}, err
		}
	}

	// For security of encrypted messagges, When both encryption and compression are enabled,
	// compression setting will be ignored and message will not be compressed.
	// Refer https://docs.ceph.com/en/quincy/rados/configuration/msgr2/#confval-ms_compress_secure
	networkSpec := instance.Spec.Network
	if networkSpec != nil && networkSpec.Connections != nil &&
		networkSpec.Connections.Encryption != nil && networkSpec.Connections.Encryption.Enabled &&
		networkSpec.Connections.Compression != nil && networkSpec.Connections.Compression.Enabled {
		r.Log.Info("Both in-transit encryption & compression are enabled. " +
			"To protect security of encrypted messages ceph will ignore compression")
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, "EncryptionAndCompressionEnabled",
			"Both in-transit encryption & compression are enabled. "+
				"To protect security of encrypted messages ceph will ignore compression")

	}

	return reconcile.Result{}, nil
}

// versionCheck populates the `.Status.Version` field
func versionCheck(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if sc.Status.Version == "" {
		sc.Status.Version = version.Version
	} else if sc.Status.Version != version.Version { // check anything else only if the versions mismatch
		storClustSemV1, err := semver.Make(sc.Status.Version)
		if err != nil {
			reqLogger.Error(err, "Error while parsing Storage Cluster version")
			return err
		}
		ocsSemV1, err := semver.Make(version.Version)
		if err != nil {
			reqLogger.Error(err, "Error while parsing OCS Operator version")
			return err
		}
		// if the storage cluster version is higher than the invoking OCS Operator's version,
		// return error
		if storClustSemV1.GT(ocsSemV1) {
			err = fmt.Errorf("Storage cluster version (%s) is higher than the OCS Operator version (%s)",
				sc.Status.Version, version.Version)
			reqLogger.Error(err, "Incompatible Storage cluster version")
			return err
		}
		// if the storage cluster version is less than the OCS Operator version,
		// just update.
		sc.Status.Version = version.Version
	}
	return nil
}

func (r *StorageClusterReconciler) SetOperatorConditions(message string, reason string, isUpgradeable metav1.ConditionStatus, prevError error) error {
	prevError = client.IgnoreNotFound(prevError)
	operatorConditionErr := r.OperatorCondition.Set(context.TODO(), isUpgradeable, conditions.Option(conditions.WithMessage(message)), conditions.Option(conditions.WithReason(reason)))
	if operatorConditionErr != nil {
		r.Log.Error(operatorConditionErr, "Unable to update OperatorCondition")
	}
	if prevError != nil && operatorConditionErr != nil {
		return error1.New(prevError.Error() + operatorConditionErr.Error())
	} else if prevError != nil {
		return prevError
	}
	return operatorConditionErr
}

// validateStorageDeviceSets checks the StorageDeviceSets of the given
// StorageCluster for completeness and correctness
func (r *StorageClusterReconciler) validateStorageDeviceSets(sc *ocsv1.StorageCluster) error {
	maxOSDSize := resource.MustParse("16Ti")

	for i, ds := range sc.Spec.StorageDeviceSets {
		if ds.DataPVCTemplate.Spec.StorageClassName == nil || *ds.DataPVCTemplate.Spec.StorageClassName == "" {
			return fmt.Errorf("failed to validate StorageDeviceSet %d: no StorageClass specified", i)
		}
		if ds.MetadataPVCTemplate != nil {
			if ds.MetadataPVCTemplate.Spec.StorageClassName == nil || *ds.MetadataPVCTemplate.Spec.StorageClassName == "" {
				return fmt.Errorf("failed to validate StorageDeviceSet %d: no StorageClass specified for metadataPVCTemplate", i)
			}
		}
		if ds.WalPVCTemplate != nil {
			if ds.WalPVCTemplate.Spec.StorageClassName == nil || *ds.WalPVCTemplate.Spec.StorageClassName == "" {
				return fmt.Errorf("failed to validate StorageDeviceSet %d: no StorageClass specified for walPVCTemplate", i)
			}
		}
		// Validate OSD size does not exceed 16Ti
		if storageRequest, exists := ds.DataPVCTemplate.Spec.Resources.Requests[corev1.ResourceStorage]; exists {
			if storageRequest.Cmp(maxOSDSize) > 0 {
				return fmt.Errorf("failed to validate StorageDeviceSet %d: OSD size %s exceeds maximum allowed size of %s", i, storageRequest.String(), maxOSDSize.String())
			}
		}
		if ds.DeviceType != "" {
			if (DeviceTypeSSD != strings.ToLower(ds.DeviceType)) && (DeviceTypeHDD != strings.ToLower(ds.DeviceType)) && (DeviceTypeNVMe != strings.ToLower(ds.DeviceType)) {
				return fmt.Errorf("failed to validate DeviceType %q: no Device of this type", ds.DeviceType)
			}
		}
	}

	return nil
}

func (r *StorageClusterReconciler) isActiveStorageCluster(instance *ocsv1.StorageCluster) bool {

	// instance is already marked for deletion
	// do not mark it as active
	if !instance.GetDeletionTimestamp().IsZero() {
		return false
	}

	// Ensure that the internal storageCluster is only allowed in the OperatorNamespace.
	if !instance.Spec.ExternalStorage.Enable && instance.Namespace != r.OperatorNamespace {
		return false
	}

	// Do not allow Multiple Storage Clusters with same name
	if r.clusters.HasMultipleStorageClustersWithSameName(instance.Name) {
		return false
	}

	// Do not allow Multiple Storage Clusters in same namespace
	if r.clusters.HasMultipleStorageClustersInNamespace(instance.Namespace) &&
		!r.isStorageClusterNotIgnored(instance, r.clusters.GetStorageClustersInNamespace(instance.Namespace)) {
		return false
	}

	var storageClusterList []ocsv1.StorageCluster
	if !instance.Spec.ExternalStorage.Enable {
		storageClusterList = r.clusters.GetInternalStorageClusters()
	} else {
		storageClusterList = r.clusters.GetExternalStorageClusters()
	}

	// There is only one StorageCluster i.e. instance
	if len(storageClusterList) == 1 {
		return true
	}

	return r.isStorageClusterNotIgnored(instance, storageClusterList)
}

func (r *StorageClusterReconciler) isStorageClusterNotIgnored(
	instance *ocsv1.StorageCluster, storageClusters []ocsv1.StorageCluster) bool {

	// There are many StorageClusters. Check if this is Active
	for n, storageCluster := range storageClusters {
		if storageCluster.Status.Phase != statusutil.PhaseIgnored &&
			storageCluster.ObjectMeta.Name != instance.ObjectMeta.Name {
			// Both StorageClusters are in creation phase
			// Tiebreak using CreationTimestamp and Alphanumeric ordering
			if storageCluster.Status.Phase == "" {
				if storageCluster.CreationTimestamp.Before(&instance.CreationTimestamp) {
					return false
				} else if storageCluster.CreationTimestamp.Equal(&instance.CreationTimestamp) && storageCluster.Name < instance.Name {
					return false
				}
				if n == len(storageClusters)-1 {
					return true
				}
				continue
			}
			return false
		}
	}

	return true
}

func (r *StorageClusterReconciler) ownStorageClusterPeersInNamespace(instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	storageClusterPeerList := &ocsv1.StorageClusterPeerList{}
	err := r.Client.List(r.ctx, storageClusterPeerList, client.InNamespace(instance.Namespace))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list storageClusterPeer: %w", err)
	}
	for i := range storageClusterPeerList.Items {
		scp := &storageClusterPeerList.Items[i]
		lenOwners := len(scp.OwnerReferences)
		err := controllerutil.SetOwnerReference(instance, scp, r.Scheme)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to set owner reference on StorageClusterPeer %v: %w", scp.Name, err)
		}
		if lenOwners != len(scp.OwnerReferences) {
			err = r.Client.Update(r.ctx, scp)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to persist StorageCluster owner ref on StorageClusterPeer %v: %w", scp.Name, err)
			}
		}
	}
	return reconcile.Result{}, nil
}

func (r *StorageClusterReconciler) ownStorageConsumersInNamespace(instance *ocsv1.StorageCluster) (reconcile.Result, error) {
	storageConsumerList := &ocsv1alpha1.StorageConsumerList{}
	err := r.Client.List(r.ctx, storageConsumerList, client.InNamespace(instance.Namespace))
	if err != nil {
		return reconcile.Result{}, fmt.Errorf("failed to list storageConsumer: %w", err)
	}
	for i := range storageConsumerList.Items {
		scp := &storageConsumerList.Items[i]
		lenOwners := len(scp.OwnerReferences)
		err := controllerutil.SetOwnerReference(instance, scp, r.Scheme)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("failed to set owner reference on storageConsumer %v: %w", scp.Name, err)
		}
		if lenOwners != len(scp.OwnerReferences) {
			err = r.Client.Update(r.ctx, scp)
			if err != nil {
				return reconcile.Result{}, fmt.Errorf("failed to persist StorageCluster owner ref on storageConsumer %v: %w", scp.Name, err)
			}
		}
	}
	return reconcile.Result{}, nil
}

// Checks whether a string is contained within a slice
func contains(slice []string, s string) bool {
	for _, item := range slice {
		if item == s {
			return true
		}
	}
	return false
}

// Removes a given string from a slice and returns the new slice
func remove(slice []string, s string) (result []string) {
	for _, item := range slice {
		if item == s {
			continue
		}
		result = append(result, item)
	}
	return
}

func validateArbiterSpec(sc *ocsv1.StorageCluster) error {

	if sc.Spec.Arbiter.Enable && sc.Spec.FlexibleScaling {
		return fmt.Errorf("arbiter and flexibleScaling both can't be enabled")
	}
	if sc.Spec.Arbiter.Enable && sc.Spec.NodeTopologies.ArbiterLocation == "" {
		return fmt.Errorf("arbiter is set to enable but no arbiterLocation has been provided in the Spec.NodeTopologies.ArbiterLocation")
	}
	return nil
}

func validateOverprovisionControlSpec(sc *ocsv1.StorageCluster) error {
	for _, opc := range sc.Spec.OverprovisionControl {
		val, ok := opc.Capacity.AsInt64()
		if !ok {
			return fmt.Errorf("non-valid capacity %v for overprovision %s", opc.Capacity, opc.QuotaName)
		}
		if val <= 0 {
			return fmt.Errorf("capacity can not be a zero or negative value for overprovision %s", opc.QuotaName)
		}
		if opc.StorageClassName == "" {
			return fmt.Errorf("missing storageclassname")
		}
		if opc.QuotaName == "" {
			return fmt.Errorf("missing quotaname")
		}
	}
	return nil
}

func validateCustomStorageClassNames(sc *ocsv1.StorageCluster) error {
	scMap := make(map[string]bool)
	duplicateNames := []string{}
	if sc.Spec.ManagedResources.CephBlockPools.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.ManagedResources.CephBlockPools.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "CephBlockPools")
		}
		scMap[sc.Spec.ManagedResources.CephBlockPools.StorageClassName] = true
	}
	if sc.Spec.ManagedResources.CephFilesystems.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.ManagedResources.CephFilesystems.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "CephFilesystems")
		}
		scMap[sc.Spec.ManagedResources.CephFilesystems.StorageClassName] = true
	}
	if sc.Spec.ManagedResources.CephNonResilientPools.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.ManagedResources.CephNonResilientPools.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "CephNonResilientPools")
		}
		scMap[sc.Spec.ManagedResources.CephNonResilientPools.StorageClassName] = true
	}
	if sc.Spec.ManagedResources.CephObjectStores.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.ManagedResources.CephObjectStores.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "CephObjectStores")
		}
		scMap[sc.Spec.ManagedResources.CephObjectStores.StorageClassName] = true
	}

	if sc.Spec.NFS != nil && sc.Spec.NFS.Enable && sc.Spec.NFS.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.NFS.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "NFS")
		}
		scMap[sc.Spec.NFS.StorageClassName] = true
	}
	if sc.Spec.Encryption.StorageClass && sc.Spec.Encryption.KeyManagementService.Enable && sc.Spec.Encryption.StorageClassName != "" {
		if _, ok := scMap[sc.Spec.Encryption.StorageClassName]; ok {
			duplicateNames = append(duplicateNames, "Encryption")
		}
		scMap[sc.Spec.Encryption.StorageClassName] = true
	}

	if len(duplicateNames) > 0 {
		return fmt.Errorf("Duplicate StorageClass name(s) provided: %v", duplicateNames)
	}

	return nil
}

func getUnsupportedClientsCount(r *StorageClusterReconciler, namespace string) (int, error) {
	scList := &ocsv1alpha1.StorageConsumerList{}
	err := r.Client.List(r.ctx, scList, client.InNamespace(namespace))
	if err != nil {
		r.Log.Error(err, "Failed to list StorageConsumers")
		return -1, err
	}
	var count int
	providerVersion, _ := semver.Make(version.Version)
	for idx := range scList.Items {
		if scList.Items[idx].Status.Client != nil {
			clientVersion, err := semver.Make(scList.Items[idx].Status.Client.OperatorVersion)
			if err == nil {
				if providerVersion.Major != clientVersion.Major || providerVersion.Minor > clientVersion.Minor {
					count++
				}
			} else {
				r.Log.Error(err, "Failed to parse client operator version", "StorageConsumer", scList.Items[idx].GetName())
				count++
			}
		}
	}

	return count, nil
}
