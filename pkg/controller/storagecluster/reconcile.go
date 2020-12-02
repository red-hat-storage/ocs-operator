package storagecluster

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/openshift/ocs-operator/version"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ReconcileStrategy is a string representing how we want to reconcile
// (or not) a particular resource
type ReconcileStrategy string

// StorageClassProvisionerType is a string representing StorageClass Provisioner. E.g: aws-ebs
type StorageClassProvisionerType string

// ensureFunc which encapsulate all the 'ensure*' type functions
type ensureFunc func(*ocsv1.StorageCluster, logr.Logger) error

const (
	rookConfigMapName = "rook-config-override"
	rookConfigData    = `
[global]
mon_osd_full_ratio = .85
mon_osd_backfillfull_ratio = .8
mon_osd_nearfull_ratio = .75
[osd]
osd_memory_target_cgroup_limit_ratio = 0.5
`
	monCountOverrideEnvVar = "MON_COUNT_OVERRIDE"
	// EBS represents AWS EBS provisioner for StorageClass
	EBS StorageClassProvisionerType = "kubernetes.io/aws-ebs"
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
	// ReconcileStrategyStandalone also means never reconcile (NooBaa)
	ReconcileStrategyStandalone ReconcileStrategy = "standalone"

	// DeviceTypeSSD represents the DeviceType SSD
	DeviceTypeSSD = "ssd"

	// DeviceTypeHDD represents the DeviceType HDD
	DeviceTypeHDD = "hdd"

	// DeviceTypeNVMe represents the DeviceType NVMe
	DeviceTypeNVMe = "nvme"
)

var storageClusterFinalizer = "storagecluster.ocs.openshift.io"

var validTopologyLabelKeys = []string{
	"failure-domain.beta.kubernetes.io",
	"failure-domain.kubernetes.io",
	"kubernetes.io/hostname",
	"topology.rook.io",
}

var throttleDiskTypes = []string{"gp2", "io1"}

// Reconcile reads that state of the cluster for a StorageCluster object and makes changes based on the state read
// and what is in the StorageCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileStorageCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := r.reqLogger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)

	// Fetch the StorageCluster instance
	instance := &ocsv1.StorageCluster{}
	err := r.client.Get(context.TODO(), request.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("No StorageCluster resource")
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return reconcile.Result{}, err
	}

	if instance.Spec.ExternalStorage.Enable {
		reqLogger.Info("Reconciling external StorageCluster")
	} else {
		reqLogger.Info("Reconciling StorageCluster")
	}

	if err := versionCheck(instance, reqLogger); err != nil {
		return reconcile.Result{}, err
	}

	// Check for active StorageCluster only if Create request is made
	// and ignore it if there's another active StorageCluster
	// If Update request is made and StorageCluster is PhaseIgnored, no need to
	// proceed further
	if instance.Status.Phase == "" {
		isActive, err := r.isActiveStorageCluster(instance)
		if err != nil {
			reqLogger.Error(err, "StorageCluster could not be reconciled. Retrying")
			return reconcile.Result{}, err
		}
		if !isActive {
			instance.Status.Phase = statusutil.PhaseIgnored
			phaseErr := r.client.Status().Update(context.TODO(), instance)
			if phaseErr != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseProgressing")
				return reconcile.Result{}, phaseErr
			}
			return reconcile.Result{}, nil
		}
	} else if instance.Status.Phase == statusutil.PhaseIgnored {
		return reconcile.Result{}, nil
	}

	if !instance.Spec.ExternalStorage.Enable {
		err = r.validateStorageDeviceSets(instance)
		if err != nil {
			reqLogger.Error(err, "Failed to validate StorageDeviceSets")
			return reconcile.Result{}, err
		}
	}

	if instance.Status.Phase != statusutil.PhaseReady &&
		instance.Status.Phase != statusutil.PhaseClusterExpanding &&
		instance.Status.Phase != statusutil.PhaseDeleting &&
		instance.Status.Phase != statusutil.PhaseConnecting {
		instance.Status.Phase = statusutil.PhaseProgressing
		phaseErr := r.client.Status().Update(context.TODO(), instance)
		if phaseErr != nil {
			reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseProgressing")
		}
	}

	// Add conditions if there are none
	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing StorageCluster"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to add conditions to status")
			return reconcile.Result{}, err
		}
	}

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if instance.GetDeletionTimestamp().IsZero() {
		if !contains(instance.GetFinalizers(), storageClusterFinalizer) {
			reqLogger.Info("Finalizer not found for storagecluster. Adding finalizer")
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to update storagecluster with finalizer")
				return reconcile.Result{}, err
			}
		}

		err = r.reconcileUninstallAnnotations(instance, reqLogger)
		if err != nil {
			return reconcile.Result{}, err
		}

	} else {
		// The object is marked for deletion
		instance.Status.Phase = statusutil.PhaseDeleting
		phaseErr := r.client.Status().Update(context.TODO(), instance)
		if phaseErr != nil {
			reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseDeleting")
		}

		if contains(instance.GetFinalizers(), storageClusterFinalizer) {
			err = r.deleteResources(instance, reqLogger)
			if err != nil {
				reqLogger.Info("Uninstall in progress", "Status", err)
				return reconcile.Result{RequeueAfter: time.Second * time.Duration(1)}, nil
			}
			reqLogger.Info("Removing finalizer")
			// Once all finalizers have been removed, the object will be deleted
			instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to remove finalizer from storagecluster")
				return reconcile.Result{}, err
			}
		}
		reqLogger.Info("Object is terminated, skipping reconciliation")
		return reconcile.Result{}, nil
	}

	if !instance.Spec.ExternalStorage.Enable {
		// Get storage node topology labels
		if err := r.reconcileNodeTopologyMap(instance, reqLogger); err != nil {
			reqLogger.Error(err, "Failed to set node topology map")
			return reconcile.Result{}, err
		}

		if instance.Status.FailureDomain == "" {
			instance.Status.FailureDomain = determineFailureDomain(instance)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set failure domain")
				return reconcile.Result{}, err
			}
		}
	}

	// in-memory conditions should start off empty. It will only ever hold
	// negative conditions (!Available, Degraded, Progressing)
	r.conditions = nil
	// Start with empty r.phase
	r.phase = ""
	var ensureFs []ensureFunc
	if !instance.Spec.ExternalStorage.Enable {
		// list of default ensure functions
		ensureFs = []ensureFunc{
			// Add support for additional resources here
			r.ensureStorageClasses,
			r.ensureSnapshotClasses,
			r.ensureCephObjectStores,
			r.ensureCephObjectStoreUsers,
			r.ensureCephBlockPools,
			r.ensureCephFilesystems,
			r.ensureCephConfig,
			r.ensureCephCluster,
			r.ensureNoobaaSystem,
			r.ensureJobTemplates,
		}
	} else {
		// for external cluster, we have a different set of ensure functions
		ensureFs = []ensureFunc{
			r.ensureExternalStorageClusterResources,
			r.ensureCephCluster,
			r.ensureSnapshotClasses,
			r.ensureNoobaaSystem,
		}
	}
	for _, f := range ensureFs {
		err = f(instance, reqLogger)
		if r.phase == statusutil.PhaseClusterExpanding {
			instance.Status.Phase = statusutil.PhaseClusterExpanding
			phaseErr := r.client.Status().Update(context.TODO(), instance)
			if phaseErr != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseClusterExpanding")
			}
		} else {
			if instance.Status.Phase != statusutil.PhaseReady &&
				instance.Status.Phase != statusutil.PhaseConnecting {
				instance.Status.Phase = statusutil.PhaseProgressing
				phaseErr := r.client.Status().Update(context.TODO(), instance)
				if phaseErr != nil {
					reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseProgressing")
				}
			}
		}
		if err != nil {
			reason := ocsv1.ReconcileFailed
			message := fmt.Sprintf("Error while reconciling: %v", err)
			statusutil.SetErrorCondition(&instance.Status.Conditions, reason, message)
			instance.Status.Phase = statusutil.PhaseError
			// don't want to overwrite the actual reconcile failure
			uErr := r.client.Status().Update(context.TODO(), instance)
			if uErr != nil {
				reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to update status")
			}
			return reconcile.Result{}, err
		}
	}
	// All component operators are in a happy state.
	if r.conditions == nil {
		reqLogger.Info("No component operator reported negatively")
		reason := ocsv1.ReconcileCompleted
		message := ocsv1.ReconcileCompletedMessage
		statusutil.SetCompleteCondition(&instance.Status.Conditions, reason, message)

		// If no operator whose conditions we are watching reports an error, then it is safe
		// to set readiness.
		r := statusutil.NewFileReady()
		err = r.Set()
		if err != nil {
			reqLogger.Error(err, "Failed to mark operator ready")
			return reconcile.Result{}, err
		}
		if instance.Status.Phase != statusutil.PhaseClusterExpanding && !instance.Spec.ExternalStorage.Enable {
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
			r := statusutil.NewFileReady()
			err = r.Unset()
			if err != nil {
				reqLogger.Error(err, "Failed to mark operator unready")
				return reconcile.Result{}, err
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
	phaseErr := r.client.Status().Update(context.TODO(), instance)
	if phaseErr != nil {
		reqLogger.Info("Status Update Error", "StatusUpdateErr", "Failed to set PhaseProgressing")
		return reconcile.Result{}, phaseErr
	}

	// enable metrics exporter at the end of reconcile
	// this allows storagecluster to be instantiated before
	// scraping metrics
	err = r.enableMetricsExporter(instance)
	if err != nil {
		reqLogger.Error(err, "failed to reconcile metrics exporter")
		return reconcile.Result{}, err
	}

	err = r.enablePrometheusRules(instance.Spec.ExternalStorage.Enable)
	if err != nil {
		reqLogger.Error(err, "failed to reconcile prometheus rules")
		return reconcile.Result{}, err
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
func (r *ReconcileStorageCluster) validateStorageDeviceSets(sc *ocsv1.StorageCluster) error {
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
			if ( DeviceTypeSSD == strings.ToLower(ds.DeviceType )) || ( DeviceTypeHDD == strings.ToLower(ds.DeviceType )) || ( DeviceTypeNVMe == strings.ToLower(ds.DeviceType )) {
				metav1.SetMetaDataAnnotation(&sc.ObjectMeta, "crushDeviceClass", ds.DeviceType)
			} else {
				return fmt.Errorf("failed to validate DeviceType %q: no Device of this type", ds.DeviceType)
			}
		}
	}

	return nil
}

// ensureCephConfig ensures that a ConfigMap resource exists with its Spec in
// the desired state.
func (r *ReconcileStorageCluster) ensureCephConfig(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
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
			"config": rookConfigData,
		},
	}

	found := &corev1.ConfigMap{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: rookConfigMapName, Namespace: sc.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating Ceph ConfigMap")
			err = r.client.Create(context.TODO(), cm)
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
	if ok != true || val != rookConfigData || ownerRefFound != true {
		reqLogger.Info("Updating Ceph ConfigMap")
		return r.client.Update(context.TODO(), cm)
	}
	return nil
}

func (r *ReconcileStorageCluster) throttleStorageDevices(storageClassName string) (bool, error) {
	storageClass := &storagev1.StorageClass{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: storageClassName}, storageClass)
	if err != nil {
		return false, fmt.Errorf("failed to retrieve StorageClass %q. %+v", storageClassName, err)
	}
	switch storageClass.Provisioner {
	case string(EBS):
		if contains(throttleDiskTypes, storageClass.Parameters["type"]) {
			return true, nil
		}
	}
	return false, nil
}

func (r *ReconcileStorageCluster) isActiveStorageCluster(instance *ocsv1.StorageCluster) (bool, error) {
	storageClusterList := ocsv1.StorageClusterList{}

	// instance is already marked for deletion
	// do not mark it as active
	if !instance.GetDeletionTimestamp().IsZero() {
		return false, nil
	}

	err := r.client.List(context.TODO(), &storageClusterList, client.InNamespace(instance.Namespace))
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

// ensureJobTemplates ensures if the osd removal job template exists
func (r *ReconcileStorageCluster) ensureJobTemplates(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	osdCleanUpTemplate := &openshiftv1.Template{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-osd-removal",
			Namespace: sc.Namespace,
		},
	}
	_, err := controllerutil.CreateOrUpdate(context.TODO(), r.client, osdCleanUpTemplate, func() error {
		osdCleanUpTemplate.Objects = []runtime.RawExtension{
			{
				Object: newCleanupJob(sc),
			},
		}
		osdCleanUpTemplate.Parameters = []openshiftv1.Parameter{
			{
				Name:        "FAILED_OSD_IDS",
				DisplayName: "OSD IDs",
				Required:    true,
				Description: `
The parameter OSD IDs needs a comma-separated list of numerical FAILED_OSD_IDs 
when a single job removes multiple OSDs. 
If the expected comma-separated format is not used, 
or an ID cannot be converted to an int, 
or if an OSD ID is not found, errors will be generated in the log and no OSDs would be removed.`,
			},
		}
		return controllerutil.SetControllerReference(sc, osdCleanUpTemplate, r.scheme)
	})
	if err != nil && !errors.IsAlreadyExists(err) {
		return fmt.Errorf("failed to create Template: %v", err.Error())
	}
	return nil
}

func newCleanupJob(sc *ocsv1.StorageCluster) *batchv1.Job {
	labels := map[string]string{
		"app": "ceph-toolbox-job-${FAILED_OSD_IDS}",
	}

	// Annotation template.alpha.openshift.io/wait-for-ready ensures template readiness
	annotations := map[string]string{
		"template.alpha.openshift.io/wait-for-ready": "true",
	}

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:        "ocs-osd-removal-${FAILED_OSD_IDS}",
			Namespace:   sc.Namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					RestartPolicy:      corev1.RestartPolicyNever,
					ServiceAccountName: "rook-ceph-system",
					Volumes: []corev1.Volume{
						{
							Name:         "ceph-conf-emptydir",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
						{
							Name:         "rook-config",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},

					Containers: []corev1.Container{
						{
							Name:  "operator",
							Image: os.Getenv("ROOK_CEPH_IMAGE"),
							Args: []string{
								"ceph",
								"osd",
								"remove",
								"--osd-ids=${FAILED_OSD_IDS}",
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "ceph-conf-emptydir",
									MountPath: "/etc/ceph",
								},
								{
									Name:      "rook-config",
									MountPath: "/var/lib/rook",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "ROOK_MON_ENDPOINTS",
									ValueFrom: &corev1.EnvVarSource{
										ConfigMapKeyRef: &corev1.ConfigMapKeySelector{
											Key:                  "data",
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon-endpoints"},
										},
									},
								},
								{
									Name:  "POD_NAMESPACE",
									Value: sc.Namespace,
								},
								{
									Name: "ROOK_CEPH_USERNAME",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key:                  "ceph-username",
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
										},
									},
								},
								{
									Name: "ROOK_CEPH_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key:                  "ceph-secret",
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
										},
									},
								},
								{
									Name: "ROOK_FSID",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key:                  "fsid",
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
										},
									},
								},
								{
									Name:  "ROOK_CONFIG_DIR",
									Value: "/var/lib/rook",
								},
								{
									Name:  "ROOK_CEPH_CONFIG_OVERRIDE",
									Value: "/etc/rook/config/override.conf",
								},
								{
									Name:  "ROOK_LOG_LEVEL",
									Value: "DEBUG",
								},
							},
						},
					},
				},
			},
		},
	}

	return job
}
