package storagecluster

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	statusutil "github.com/openshift/ocs-operator/controllers/util"
	"github.com/openshift/ocs-operator/version"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileStrategy is a string representing how we want to reconcile
// (or not) a particular resource
type ReconcileStrategy string

// StorageClassProvisionerType is a string representing StorageClass Provisioner. E.g: aws-ebs
type StorageClassProvisionerType string

type resourceManager interface {
	ensureCreated(*StorageClusterReconciler, *ocsv1.StorageCluster) error
	ensureDeleted(*StorageClusterReconciler, *ocsv1.StorageCluster) error
}

type ocsCephConfig struct{}
type ocsJobTemplates struct{}

const (
	rookConfigMapName     = "rook-config-override"
	defaultRookConfigData = `
[global]
mon_osd_full_ratio = .85
mon_osd_backfillfull_ratio = .8
mon_osd_nearfull_ratio = .75
mon_max_pg_per_osd = 600
[osd]
osd_memory_target_cgroup_limit_ratio = 0.5
`
	monCountOverrideEnvVar = "MON_COUNT_OVERRIDE"

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
	// ReconcileStrategyForce means always reconcile, regardless of platform
	ReconcileStrategyForce ReconcileStrategy = "force"
	// ReconcileStrategyStandalone also means never reconcile (NooBaa)
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
)

var storageClusterFinalizer = "storagecluster.ocs.openshift.io"

const labelZoneRegionWithoutBeta = "failure-domain.kubernetes.io/region"
const labelZoneFailureDomainWithoutBeta = "failure-domain.kubernetes.io/zone"
const labelRookPrefix = "topology.rook.io"

var validTopologyLabelKeys = []string{
	// This is the most preferred key as kubernetes recommends zone and region
	// labels under this key.
	corev1.LabelZoneRegionStable,

	// These two are retained only to have backward compatibility; they are
	// deprecated by kubernetes. If topology.kubernetes.io key has same label we
	// will skip the next two from the topologyMap.
	corev1.LabelZoneRegion,
	labelZoneRegionWithoutBeta,

	// This is the most preferred key as kubernetes recommends zone and region
	// labels under this key.
	corev1.LabelZoneFailureDomainStable,

	// These two are retained only to have backward compatibility; they are
	// deprecated by kubernetes. If topology.kubernetes.io key has same label we
	// will skip the next two from the topologyMap.
	corev1.LabelZoneFailureDomain,
	labelZoneFailureDomainWithoutBeta,

	// This is the kubernetes recommended label to select nodes.
	corev1.LabelHostname,

	// This label is used to assign rack based topology.
	labelRookPrefix,
}

// +kubebuilder:rbac:groups=ocs.openshift.io,resources=*,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclusters;cephblockpools;cephfilesystems;cephobjectstores;cephobjectstoreusers,verbs=*
// +kubebuilder:rbac:groups=noobaa.io,resources=noobaas,verbs=*
// +kubebuilder:rbac:groups=storage.k8s.io,resources=storageclasses,verbs=*
// +kubebuilder:rbac:groups=core,resources=pods;services;endpoints;persistentvolumeclaims;events;configmaps;secrets;nodes,verbs=*
// +kubebuilder:rbac:groups=core,resources=namespaces,verbs=get
// +kubebuilder:rbac:groups=apps,resources=deployments;daemonsets;replicasets;statefulsets,verbs=*
// +kubebuilder:rbac:groups=monitoring.coreos.com,resources=servicemonitors;prometheusrules,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=snapshot.storage.k8s.io,resources=volumesnapshots;volumesnapshotclasses,verbs=*
// +kubebuilder:rbac:groups=template.openshift.io,resources=templates,verbs=*
// +kubebuilder:rbac:groups=config.openshift.io,resources=infrastructures,verbs=get;list;watch
// +kubebuilder:rbac:groups=console.openshift.io,resources=consolequickstarts,verbs=*
// +kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=route.openshift.io,resources=routes,verbs=*
// +kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;create;update

// Reconcile reads that state of the cluster for a StorageCluster object and makes changes based on the state read
// and what is in the StorageCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *StorageClusterReconciler) Reconcile(ctx context.Context, request reconcile.Request) (reconcile.Result, error) {

	prevLogger := r.Log
	defer func() { r.Log = prevLogger }()
	r.Log = r.Log.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the StorageCluster instance
	sc := &ocsv1.StorageCluster{}
	if err := r.Client.Get(ctx, request.NamespacedName, sc); err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("No StorageCluster resource")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if err := r.validateStorageClusterSpec(sc, request); err != nil {
		return reconcile.Result{}, err
	}

	// Reconcile changes to the cluster
	result, reconcileError := r.reconcilePhases(sc, request)

	// Apply status changes to the storagecluster
	statusError := r.Client.Status().Update(ctx, sc)
	if statusError != nil {
		r.Log.Info("Status Update Error", "StatusUpdateErr", "Could not update storagecluster status")
	}

	// Reconcile errors have higher priority than status update errors
	if reconcileError != nil {
		return result, reconcileError
	} else if statusError != nil {
		return result, statusError
	} else {
		return result, nil
	}
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

// validateStorageClusterSpec must be called before reconciling. Any syntactic and sematic errors in the CR must be caught here.
func (r *StorageClusterReconciler) validateStorageClusterSpec(instance *ocsv1.StorageCluster, request reconcile.Request) error {
	if err := versionCheck(instance, r.Log); err != nil {
		r.Log.Error(err, "Failed to validate version")
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			return updateErr
		}
		return err
	}

	if !instance.Spec.ExternalStorage.Enable {
		if err := r.validateStorageDeviceSets(instance); err != nil {
			r.Log.Error(err, "Failed to validate StorageDeviceSets")
			r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
			instance.Status.Phase = statusutil.PhaseError
			if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
				return updateErr
			}
			return err
		}
	}

	if err := validateArbiterSpec(instance, r.Log); err != nil {
		r.Log.Error(err, "Failed to validate ArbiterSpec")
		r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonValidationFailed, err.Error())
		instance.Status.Phase = statusutil.PhaseError
		if updateErr := r.Client.Status().Update(context.TODO(), instance); updateErr != nil {
			return updateErr
		}
		return err
	}
	return nil
}

func (r *StorageClusterReconciler) reconcilePhases(
	instance *ocsv1.StorageCluster,
	request reconcile.Request) (reconcile.Result, error) {

	if instance.Spec.ExternalStorage.Enable {
		r.Log.Info("Reconciling external StorageCluster")
	} else {
		r.Log.Info("Reconciling StorageCluster")
	}

	// Initialize the StatusImages section of the storageclsuter CR
	r.initializeImagesStatus(instance)

	// Check for active StorageCluster only if Create request is made
	// and ignore it if there's another active StorageCluster
	// If Update request is made and StorageCluster is PhaseIgnored, no need to
	// proceed further
	if instance.Status.Phase == "" {
		isActive, err := r.isActiveStorageCluster(instance)
		if err != nil {
			r.Log.Error(err, "StorageCluster could not be reconciled. Retrying")
			return reconcile.Result{}, err
		}
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
		instance.Status.Phase != statusutil.PhaseConnecting {
		instance.Status.Phase = statusutil.PhaseProgressing
	}

	// Add conditions if there are none
	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing StorageCluster"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)
	}

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if instance.GetDeletionTimestamp().IsZero() {
		if !contains(instance.GetFinalizers(), storageClusterFinalizer) {
			r.Log.Info("Finalizer not found for storagecluster. Adding finalizer")
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				r.Log.Info("Update Error", "MetaUpdateErr", "Failed to update storagecluster with finalizer")
				return reconcile.Result{}, err
			}
		}

		if err := r.reconcileUninstallAnnotations(instance); err != nil {
			return reconcile.Result{}, err
		}

	} else {
		// The object is marked for deletion
		instance.Status.Phase = statusutil.PhaseDeleting

		if contains(instance.GetFinalizers(), storageClusterFinalizer) {
			if err := r.deleteResources(instance); err != nil {
				r.Log.Info("Uninstall in progress", "Status", err)
				r.recorder.ReportIfNotPresent(instance, corev1.EventTypeWarning, statusutil.EventReasonUninstallPending, err.Error())
				return reconcile.Result{RequeueAfter: time.Second * time.Duration(1)}, nil
			}
			r.Log.Info("Removing finalizer")
			// Once all finalizers have been removed, the object will be deleted
			instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				r.Log.Info("Update Error", "MetaUpdateErr", "Failed to remove finalizer from storagecluster")
				return reconcile.Result{}, err
			}
		}
		r.Log.Info("Object is terminated, skipping reconciliation")
		ReadinessSet()
		return reconcile.Result{}, nil
	}

	if !instance.Spec.ExternalStorage.Enable {
		// Get storage node topology labels
		if err := r.reconcileNodeTopologyMap(instance); err != nil {
			r.Log.Error(err, "Failed to set node topology map")
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
		// list of default ensure functions
		objs = []resourceManager{
			&ocsStorageClass{},
			&ocsSnapshotClass{},
			&ocsCephObjectStores{},
			&ocsCephObjectStoreUsers{},
			&ocsCephRGWRoutes{},
			&ocsCephBlockPools{},
			&ocsCephFilesystems{},
			&ocsCephConfig{},
			&ocsCephCluster{},
			&ocsNoobaaSystem{},
			&ocsJobTemplates{},
			&ocsQuickStarts{},
		}

	} else {
		// for external cluster, we have a different set of ensure functions
		objs = []resourceManager{
			&ocsExternalResources{},
			&ocsCephCluster{},
			&ocsSnapshotClass{},
			&ocsNoobaaSystem{},
			&ocsQuickStarts{},
		}
	}

	for _, obj := range objs {
		err := obj.ensureCreated(r, instance)
		if r.phase == statusutil.PhaseClusterExpanding {
			instance.Status.Phase = statusutil.PhaseClusterExpanding
		} else if instance.Status.Phase != statusutil.PhaseReady &&
			instance.Status.Phase != statusutil.PhaseConnecting {
			instance.Status.Phase = statusutil.PhaseProgressing
		}
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)
			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			return reconcile.Result{}, err
		}
	}
	// All component operators are in a happy state.
	if r.conditions == nil {
		r.Log.Info("No component operator reported negatively")
		reason := ocsv1.ReconcileCompleted
		message := ocsv1.ReconcileCompletedMessage
		statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)

		// If no operator whose conditions we are watching reports an error, then it is safe
		// to set readiness.
		ReadinessSet()
		if instance.Status.Phase != statusutil.PhaseClusterExpanding &&
			!instance.Spec.ExternalStorage.Enable {
			instance.Status.Phase = statusutil.PhaseReady
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
			ReadinessUnset()
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
		if err := r.enableMetricsExporter(instance); err != nil {
			r.Log.Error(err, "failed to reconcile metrics exporter")
			return reconcile.Result{}, err
		}

		if err := r.enablePrometheusRules(instance); err != nil {
			r.Log.Error(err, "failed to reconcile prometheus rules")
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// versionCheck populates the `.Spec.Version` field
func versionCheck(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if sc.Spec.Version == "" {
		sc.Spec.Version = version.Version
	} else if sc.Spec.Version != version.Version { // check anything else only if the versions mis-match
		storClustSemV1, err := semver.Make(sc.Spec.Version)
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
				sc.Spec.Version, version.Version)
			reqLogger.Error(err, "Incompatible Storage cluster version")
			return err
		}
		// if the storage cluster version is less than the OCS Operator version,
		// just update.
		sc.Spec.Version = version.Version
	}
	return nil
}

// validateStorageDeviceSets checks the StorageDeviceSets of the given
// StorageCluster for completeness and correctness
func (r *StorageClusterReconciler) validateStorageDeviceSets(sc *ocsv1.StorageCluster) error {
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
		if ds.DeviceType != "" {
			if (DeviceTypeSSD != strings.ToLower(ds.DeviceType)) && (DeviceTypeHDD != strings.ToLower(ds.DeviceType)) && (DeviceTypeNVMe != strings.ToLower(ds.DeviceType)) {
				return fmt.Errorf("failed to validate DeviceType %q: no Device of this type", ds.DeviceType)
			}
		}
	}

	return nil
}

// ensureCreated ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (obj *ocsCephConfig) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephConfig.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	found := &corev1.ConfigMap{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: rookConfigMapName, Namespace: sc.Namespace}, found)

	if err == nil && reconcileStrategy == ReconcileStrategyInit {
		return nil
	}

	ownerRef := metav1.OwnerReference{
		UID:        sc.UID,
		APIVersion: sc.APIVersion,
		Kind:       sc.Kind,
		Name:       sc.Name,
	}
	cm := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:            rookConfigMapName,
			Namespace:       sc.Namespace,
			OwnerReferences: []metav1.OwnerReference{ownerRef},
		},
		Data: map[string]string{
			"config": defaultRookConfigData,
		},
	}

	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Creating Ceph ConfigMap")
			err = r.Client.Create(context.TODO(), cm)
			if err != nil {
				return err
			}
		}
		return err
	}

	ownerRefFound := false
	for _, ownerRef := range found.OwnerReferences {
		if ownerRef.UID == sc.UID {
			ownerRefFound = true
		}
	}
	val, ok := found.Data["config"]
	if !ok || val != defaultRookConfigData || !ownerRefFound {
		r.Log.Info("Updating Ceph ConfigMap")
		return r.Client.Update(context.TODO(), cm)
	}
	return nil
}

// ensureDeleted is dummy func for the ocsCephConfig
func (obj *ocsCephConfig) ensureDeleted(r *StorageClusterReconciler, instance *ocsv1.StorageCluster) error {
	return nil
}

func (r *StorageClusterReconciler) isActiveStorageCluster(instance *ocsv1.StorageCluster) (bool, error) {
	storageClusterList := ocsv1.StorageClusterList{}

	// instance is already marked for deletion
	// do not mark it as active
	if !instance.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	err := r.Client.List(context.TODO(), &storageClusterList, client.InNamespace(instance.Namespace))
	if err != nil {
		return false, fmt.Errorf("Error fetching StorageClusterList. %+v", err)
	}

	// There is only one StorageCluster i.e. instance
	if len(storageClusterList.Items) == 1 {
		return true, nil
	}

	// There are many StorageClusters. Check if this is Active
	for n, storageCluster := range storageClusterList.Items {
		if storageCluster.Status.Phase != statusutil.PhaseIgnored &&
			storageCluster.ObjectMeta.Name != instance.ObjectMeta.Name {
			// Both StorageClusters are in creation phase
			// Tiebreak using CreationTimestamp and Alphanumeric ordering
			if storageCluster.Status.Phase == "" {
				if storageCluster.CreationTimestamp.Before(&instance.CreationTimestamp) {
					return false, nil
				} else if storageCluster.CreationTimestamp.Equal(&instance.CreationTimestamp) && storageCluster.Name < instance.Name {
					return false, nil
				}
				if n == len(storageClusterList.Items)-1 {
					return true, nil
				}
				continue
			}
			return false, nil
		}
	}
	return true, nil
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

func validateArbiterSpec(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {

	if sc.Spec.Arbiter.Enable && sc.Spec.FlexibleScaling {
		return fmt.Errorf("arbiter and flexibleScaling both can't be enabled")
	}
	if sc.Spec.Arbiter.Enable && sc.Spec.NodeTopologies.ArbiterLocation == "" {
		return fmt.Errorf("arbiter is set to enable but no arbiterLocation has been provided in the Spec.NodeTopologies.ArbiterLocation")
	}
	return nil
}
