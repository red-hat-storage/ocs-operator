package storagecluster

import (
	// The embed package is required for the prometheus rule files
	_ "embed"
	"time"

	"bytes"
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	configv1 "github.com/openshift/api/config/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	monitoringclient "github.com/prometheus-operator/prometheus-operator/pkg/client/versioned"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	k8sYAML "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type ocsCephCluster struct{}
type diskSpeed string

const (
	diskSpeedUnknown diskSpeed = "unknown"
	diskSpeedSlow    diskSpeed = "slow"
	diskSpeedFast    diskSpeed = "fast"
)

const (
	poolTypeData     = "data"
	poolTypeMetadata = "metadata"
)

type knownDiskType struct {
	speed            diskSpeed
	provisioner      StorageClassProvisionerType
	storageClassType string
}

// These are known disk types where we can't correctly detect the type of the
// disk (rotational or ssd) automatically, so rook would apply wrong tunings.
// This list allows to specify disks from which storage classes to tune for fast
// or slow disk optimization.
var knownDiskTypes = []knownDiskType{
	// only gp2-csi SC is present in the installations starting with OCP 4.12
	// gp2 is still required to support upgrades from 4.11 to 4.12.
	{diskSpeedSlow, EBS, "gp2"},
	{diskSpeedSlow, EBS, "gp2-csi"},
	{diskSpeedSlow, EBS, "io1"},
	{diskSpeedFast, AzureDisk, "managed-premium"},
}

const (
	// Hardcoding networkProvider to multus and this can be changed later to accommodate other providers
	networkProvider           = "multus"
	publicNetworkSelectorKey  = "public"
	clusterNetworkSelectorKey = "cluster"
)

const (
	// PriorityClasses for cephCluster
	systemNodeCritical         = "system-node-critical"
	openshiftUserCritical      = "openshift-user-critical"
	prometheusLocalRuleName    = "prometheus-ceph-rules"
	prometheusExternalRuleName = "prometheus-ceph-rules-external"
)

var (
	//go:embed prometheus/externalcephrules.yaml
	externalPrometheusRules string
	//go:embed prometheus/localcephrules.yaml
	localPrometheusRules    string
	testSkipPrometheusRules = false
)

func arbiterEnabled(sc *ocsv1.StorageCluster) bool {
	return sc.Spec.Arbiter.Enable
}

func (r *StorageClusterReconciler) deleteJobsWithDuplicateTolerations(sc *ocsv1.StorageCluster) error {
	// delete the osd prepare jobs which are completed and have duplicate placements
	jobName := "rook-ceph-osd-prepare"
	listOpts := &client.ListOptions{
		Namespace: sc.Namespace,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"app": jobName,
		}),
	}

	jobs := &batchv1.JobList{}
	// list all the prepare jobs
	err := r.Client.List(r.ctx, jobs, listOpts)
	if err != nil {
		r.Log.Error(err, "failed to list OSD prepare pod jobs")
		return err
	}

	// filter out the completed jobs
	for _, job := range jobs.Items {
		if job.Status.Succeeded == 1 {
			// filter the jobs that has duplicated tolerations
			tolerations := job.Spec.Template.Spec.Tolerations

			duplicate := make(map[corev1.Toleration]bool)
			for _, toleration := range tolerations {
				if duplicate[toleration] {
					r.Log.Info("deleting completed OSD prepare jobs with duplicate tolerations", "name", klog.KRef(job.Namespace, job.Name))
					deletePolicy := metav1.DeletePropagationForeground
					err := r.Client.Delete(r.ctx, &job, &client.DeleteOptions{PropagationPolicy: &deletePolicy})
					if err != nil {
						r.Log.Error(err, "failed to delete OSD prepare pod job", "name", klog.KRef(job.Namespace, job.Name))
						return err
					}
					break
				}
				duplicate[toleration] = true
			}
		}
	}

	return nil
}

// ensureCreated ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (obj *ocsCephCluster) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	var (
		cephCluster       *rookCephv1.CephCluster
		err               error
		reconcileStrategy = ReconcileStrategy(sc.Spec.ManagedResources.CephCluster.ReconcileStrategy)
	)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return reconcile.Result{}, nil
	}

	if sc.Spec.ExternalStorage.Enable && len(sc.Spec.StorageDeviceSets) != 0 {
		return reconcile.Result{}, fmt.Errorf("'StorageDeviceSets' should not be initialized in an external CephCluster")
	}

	// TODO: remove the fix in the next release 4.19
	// as it is needed only for the upgrade to 4.18
	if !sc.Spec.ExternalStorage.Enable {
		err = r.deleteJobsWithDuplicateTolerations(sc)
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	for i, ds := range sc.Spec.StorageDeviceSets {
		sc.Spec.StorageDeviceSets[i].Config.TuneSlowDeviceClass = false
		sc.Spec.StorageDeviceSets[i].Config.TuneFastDeviceClass = false

		// Use SSD deviceClass and TuneFastDeviceClass for all internal clusters where deviceClass is not provided by the customer
		if !sc.Spec.ExternalStorage.Enable && sc.Spec.StorageDeviceSets[i].DeviceClass == "" {
			sc.Spec.StorageDeviceSets[i].DeviceClass = DeviceTypeSSD
			sc.Spec.StorageDeviceSets[i].Config.TuneFastDeviceClass = true
			continue
		}

		diskSpeed, err := r.checkTuneStorageDevices(ds)
		if err != nil {
			return reconcile.Result{}, fmt.Errorf("Failed to check for known device types: %+v", err)
		}
		switch diskSpeed {
		case diskSpeedSlow:
			sc.Spec.StorageDeviceSets[i].Config.TuneSlowDeviceClass = true
		case diskSpeedFast:
			sc.Spec.StorageDeviceSets[i].Config.TuneFastDeviceClass = true
		default:
		}
	}

	if isMultus(sc.Spec.Network) {
		err := validateMultusSelectors(sc.Spec.Network.Selectors)
		if err != nil {
			r.Log.Error(err, "Failed to validate Multus Selectors specified in StorageCluster.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
			return reconcile.Result{}, err
		}
	}

	// Define a new CephCluster object
	if sc.Spec.ExternalStorage.Enable {
		extRArr, ok := externalOCSResources[sc.UID]
		if !ok {
			return reconcile.Result{}, fmt.Errorf("Unable to retrieve external resource from externalOCSResources")
		}
		endpointR, err := findNamedResourceFromArray(extRArr, "monitoring-endpoint")
		if err != nil {
			return reconcile.Result{}, err
		}
		monitoringIP := endpointR.Data["MonitoringEndpoint"]
		monitoringPort := endpointR.Data["MonitoringPort"]
		if err := verifyMonitoringEndpoints(monitoringIP, monitoringPort, r.Log); err != nil {
			r.Log.Error(err, "Could not connect to the Monitoring Endpoints.",
				"CephCluster", klog.KRef(sc.Namespace, sc.Name))
			// reset the external resources cache line for this storagecluster to nil,
			// to allow the next reconcile to fetch an updated  external resources information
			// from the provider cluster
			externalOCSResources[sc.UID] = nil
			return reconcile.Result{}, err
		}
		r.Log.Info("Monitoring Information found. Monitoring will be enabled on the external cluster.", "CephCluster", klog.KRef(sc.Namespace, sc.Name))
		cephCluster = newExternalCephCluster(sc, monitoringIP, monitoringPort)
	} else {
		// Add KMS details to CephCluster spec, only if
		// cluster-wide encryption is enabled or any of the device set is encrypted
		// ie, sc.Spec.Encryption.ClusterWide/sc.Spec.Encryption.Enable is True or any device is encrypted
		// and KMS ConfigMap is available

		if util.IsClusterOrDeviceSetEncrypted(sc) {
			kmsConfigMap, err := getKMSConfigMap(KMSConfigMapName, sc, r.Client)
			if err != nil {
				r.Log.Error(err, "Failed to procure KMS ConfigMap.", "KMSConfigMap", klog.KRef(sc.Namespace, KMSConfigMapName))
				return reconcile.Result{}, err
			}
			if kmsConfigMap != nil {
				// reset the KMS connection's error field,
				// it will be anyway set if there is an error
				sc.Status.KMSServerConnection.KMSServerConnectionError = ""
				// get the current KMS provider name
				currKMSProvider := kmsConfigMap.Data[KMSProviderKey]
				// according to the KMS provider, get the corresponding address/endpoint key
				// and use it to set 'KMSServerAddress' status
				kmsAddrssKey := kmsProviderAddressKeyMap[currKMSProvider]
				sc.Status.KMSServerConnection.KMSServerAddress = kmsConfigMap.Data[kmsAddrssKey]
				// if the KMS connection address is empty, log it as an error and continue
				if sc.Status.KMSServerConnection.KMSServerAddress == "" {
					r.Log.Error(nil, "An empty KMS server connection address found",
						"KMSProviderName", currKMSProvider, "KMSAddressKey", kmsAddrssKey)
				} else if err = reachKMSProvider(kmsConfigMap); err != nil {
					sc.Status.KMSServerConnection.KMSServerConnectionError = err.Error()
					r.Log.Error(err, "Address provided in KMS ConfigMap is not reachable.", "KMSConfigMap", klog.KRef(kmsConfigMap.Namespace, kmsConfigMap.Name))
					return reconcile.Result{}, err
				}
			}
			cephCluster = newCephCluster(sc, r.images.Ceph, kmsConfigMap, r.Log)
		} else {
			cephCluster = newCephCluster(sc, r.images.Ceph, nil, r.Log)
		}
	}

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.Scheme); err != nil {
		r.Log.Error(err, "Unable to set controller reference for CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
		return reconcile.Result{}, err
	}

	platform, err := platform.GetPlatformType()
	if err != nil {
		return reconcile.Result{}, err
	}

	if platform == configv1.IBMCloudPlatformType {
		r.Log.Info("Increasing Mon failover timeout to 15m.", "Platform", platform)
		cephCluster.Spec.HealthCheck.DaemonHealth.Monitor.Timeout = "15m"
	}

	ipFamily, isDualStack, err := getIPFamilyConfig(r.Client)
	if err != nil {
		r.Log.Error(err, "failed to get IPFamily of the cluster")
		return reconcile.Result{}, err
	}

	// Dual Stack is not supported in downstream ceph. BZ references:
	// https://bugzilla.redhat.com/show_bug.cgi?id=1804290
	// https://bugzilla.redhat.com/show_bug.cgi?id=2181350#c10
	// So use IPv4 and DualStack:False in case of dual stack cluster
	// No need to update the ipFamily config in the Network settings if the cluster is single Stack IPv4.
	if isDualStack {
		cephCluster.Spec.Network.IPFamily = rookCephv1.IPv4
		cephCluster.Spec.Network.DualStack = false
	} else if ipFamily == rookCephv1.IPv6 {
		cephCluster.Spec.Network.IPFamily = ipFamily
	}

	// Check if this CephCluster already exists
	found := &rookCephv1.CephCluster{}
	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			if sc.Spec.ExternalStorage.Enable {
				r.Log.Info("Creating external CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
			} else {
				r.Log.Info("Creating CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
			}
			if err := r.Client.Create(context.TODO(), cephCluster); err != nil {
				r.Log.Error(err, "Unable to create CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
				return reconcile.Result{}, err
			}
			// Need to happen after the ceph cluster CR creation was confirmed
			sc.Status.Images.Ceph.ActualImage = cephCluster.Spec.CephVersion.Image
			// Assuming progress when ceph cluster CR is created.
			reason := "CephClusterStatus"
			message := "CephCluster resource is not reporting status"
			statusutil.MapCephClusterNoConditions(&r.conditions, reason, message)
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Unable to fetch CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
		return reconcile.Result{}, err
	} else if reconcileStrategy == ReconcileStrategyInit {
		return reconcile.Result{}, nil
	}

	// Record actual Ceph container image version before attempting update
	sc.Status.Images.Ceph.ActualImage = found.Spec.CephVersion.Image

	// Update OSD store to `bluestore`
	cephCluster.Spec.Storage.Store = updateOSDStore(found.Spec.Storage.Store)

	// confirm OSD migration if encryption is enabled as day-2 operation
	if isEncrptionSettingUpdated(sc.Spec.Encryption.ClusterWide, found.Spec.Storage.StorageClassDeviceSets) {
		cephCluster.Spec.Storage.Migration.Confirmation = "yes-really-migrate-osds"
	} else {
		// keeping the same expected state in the corresponding reconcile once encryption is enabled
		cephCluster.Spec.Storage.Migration.Confirmation = found.Spec.Storage.Migration.Confirmation
	}

	// Add it to the list of RelatedObjects if found
	objectRef, err := reference.GetReference(r.Scheme, found)
	if err != nil {
		r.Log.Error(err, "Unable to get CephCluster ObjectReference.", "CephCluster", klog.KRef(found.Namespace, found.Name))
		return reconcile.Result{}, err
	}
	err = objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	if err != nil {
		r.Log.Error(err, "Unable to add CephCluster to the list of Related Objects in StorageCluster.", "CephCluster", klog.KRef(objectRef.Namespace, objectRef.Name), "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
		return reconcile.Result{}, err
	}

	// Handle CephCluster resource status
	if found.Status.State == "" {
		r.Log.Info("CephCluster resource is not reporting status.", "CephCluster", klog.KRef(found.Namespace, found.Name))
		// What does this mean to OCS status? Assuming progress.
		reason := "CephClusterStatus"
		message := "CephCluster resource is not reporting status"
		statusutil.MapCephClusterNoConditions(&r.conditions, reason, message)
	} else {
		// Interpret CephCluster status and set any negative conditions
		if sc.Spec.ExternalStorage.Enable {
			statusutil.MapExternalCephClusterNegativeConditions(&r.conditions, found)
		} else {
			statusutil.MapCephClusterNegativeConditions(&r.conditions, found)
		}
	}

	// When phase is expanding, wait for CephCluster state to be updating
	// this means expansion is in progress and overall system is progressing
	// else expansion is not yet triggered
	if sc.Status.Phase == statusutil.PhaseClusterExpanding &&
		found.Status.State != rookCephv1.ClusterStateUpdating {
		r.phase = statusutil.PhaseClusterExpanding
	}

	if sc.Spec.ExternalStorage.Enable {
		if found.Status.State == rookCephv1.ClusterStateConnecting {
			sc.Status.Phase = statusutil.PhaseConnecting
		} else if found.Status.State == rookCephv1.ClusterStateConnected {
			sc.Status.Phase = statusutil.PhaseReady
		} else {
			sc.Status.Phase = statusutil.PhaseNotReady
		}
	}

	// Update the CephCluster if it is not in the desired state
	if !reflect.DeepEqual(cephCluster.Spec, found.Spec) {
		r.Log.Info("Updating spec for CephCluster.", "CephCluster", klog.KRef(found.Namespace, found.Name))
		if !sc.Spec.ExternalStorage.Enable {
			// Check if Cluster is Expanding
			if len(found.Spec.Storage.StorageClassDeviceSets) < len(cephCluster.Spec.Storage.StorageClassDeviceSets) {
				r.phase = statusutil.PhaseClusterExpanding
			} else if len(found.Spec.Storage.StorageClassDeviceSets) == len(cephCluster.Spec.Storage.StorageClassDeviceSets) {
				for _, countInFoundSpec := range found.Spec.Storage.StorageClassDeviceSets {
					for _, countInCephClusterSpec := range cephCluster.Spec.Storage.StorageClassDeviceSets {
						if countInFoundSpec.Name == countInCephClusterSpec.Name && countInCephClusterSpec.Count > countInFoundSpec.Count {
							r.phase = statusutil.PhaseClusterExpanding
							break
						}
					}
					if r.phase == statusutil.PhaseClusterExpanding {
						break
					}
				}
			}
		}
		found.Spec = cephCluster.Spec
		if err := r.Client.Update(context.TODO(), found); err != nil {
			r.Log.Error(err, "Unable to update CephCluster.", "CephCluster", klog.KRef(found.Namespace, found.Name))
			return reconcile.Result{}, err
		}
	}

	// Set the default CephDeviceClass in the StorageCluster status if it's not set yet (not needed for external clusters)
	if !sc.Spec.ExternalStorage.Enable && sc.Status.DefaultCephDeviceClass == "" && found.Status.CephStorage != nil {
		sc.Status.DefaultCephDeviceClass = determineDefaultCephDeviceClass(found.Status.CephStorage.DeviceClasses, sc.Spec.ManagedResources.CephNonResilientPools.Enable, sc.Status.FailureDomainValues)
	}

	// Update the currentMonCount field in StoragCluster status from the cephCluster CR
	if !sc.Spec.ExternalStorage.Enable {
		sc.Status.CurrentMonCount = cephCluster.Spec.Mon.Count
	}

	// Create the prometheus rules if required by the cephcluster CR
	if err := createPrometheusRules(r, sc, cephCluster); err != nil {
		r.Log.Error(err, "Unable to create or update prometheus rules.", "CephCluster", klog.KRef(found.Namespace, found.Name))
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// ensureDeleted deletes the CephCluster owned by the StorageCluster
func (obj *ocsCephCluster) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) (reconcile.Result, error) {
	cephCluster := &rookCephv1.CephCluster{}
	cephClusterName := generateNameForCephCluster(sc)
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephClusterName, Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster not found.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return reconcile.Result{}, nil
		}
		r.Log.Error(err, "Uninstall: Unable to retrieve CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
		return reconcile.Result{}, fmt.Errorf("Uninstall: Unable to retrieve cephCluster: %v", err)
	}

	if cephCluster.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
		err = r.Client.Delete(context.TODO(), cephCluster)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return reconcile.Result{}, fmt.Errorf("uninstall: Failed to delete CephCluster: %v", err)
		}
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster is deleted.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return reconcile.Result{}, nil
		}
	}
	r.Log.Error(err, "Uninstall: Waiting for CephCluster to be deleted.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
	return reconcile.Result{}, fmt.Errorf("uninstall: Waiting for CephCluster to be deleted")

}

func getCephClusterMonitoringLabels(sc ocsv1.StorageCluster) map[string]string {
	labels := make(map[string]string)
	if sc.Spec.Monitoring != nil && sc.Spec.Monitoring.Labels != nil {
		labels = sc.Spec.Monitoring.Labels
	}
	labels["rook.io/managedBy"] = sc.Name
	return labels
}

// newCephCluster returns a CephCluster object.
func newCephCluster(sc *ocsv1.StorageCluster, cephImage string, kmsConfigMap *corev1.ConfigMap, reqLogger logr.Logger) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	maxLogSize := resource.MustParse("500Mi")

	logCollector := rookCephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	cephCluster := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephCluster(sc),
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: rookCephv1.ClusterSpec{
			CephVersion: rookCephv1.CephVersionSpec{
				Image:            cephImage,
				AllowUnsupported: allowUnsupportedCephVersion(),
			},
			Mon:             generateMonSpec(sc),
			Mgr:             generateMgrSpec(sc),
			DataDirHostPath: "/var/lib/rook",
			DisruptionManagement: rookCephv1.DisruptionManagementSpec{
				ManagePodBudgets:                 true,
				ManageMachineDisruptionBudgets:   false,
				MachineDisruptionBudgetNamespace: "openshift-machine-api",
			},
			Network: getNetworkSpec(*sc),
			Dashboard: rookCephv1.DashboardSpec{
				Enabled: sc.Spec.ManagedResources.CephDashboard.Enable,
				SSL:     sc.Spec.ManagedResources.CephDashboard.SSL,
			},
			Monitoring: rookCephv1.MonitoringSpec{
				Enabled:  true,
				Interval: &metav1.Duration{Duration: 30 * time.Second},
			},
			Storage: rookCephv1.StorageScopeSpec{
				AllowDeviceClassUpdate:       sc.Spec.ManagedResources.CephCluster.AllowDeviceClassUpdate,
				AllowOsdCrushWeightUpdate:    true,
				StorageClassDeviceSets:       newStorageClassDeviceSets(sc),
				FlappingRestartIntervalHours: 24,
				FullRatio:                    sc.Spec.ManagedResources.CephCluster.FullRatio,
				NearFullRatio:                sc.Spec.ManagedResources.CephCluster.NearFullRatio,
				BackfillFullRatio:            sc.Spec.ManagedResources.CephCluster.BackfillFullRatio,
			},
			Placement: rookCephv1.PlacementSpec{
				"all":     getPlacement(sc, "all"),
				"mon":     getPlacement(sc, "mon"),
				"arbiter": getPlacement(sc, "arbiter"),
			},
			PriorityClassNames: rookCephv1.PriorityClassNamesSpec{
				rookCephv1.KeyMgr: systemNodeCritical,
				rookCephv1.KeyMon: systemNodeCritical,
				rookCephv1.KeyOSD: systemNodeCritical,
			},
			Resources:    newCephDaemonResources(sc),
			LogCollector: logCollector,
			Labels: rookCephv1.LabelsSpec{
				rookCephv1.KeyMgr:          rookCephv1.Labels{defaults.ODFResourceProfileKey: sc.Spec.ResourceProfile},
				rookCephv1.KeyMon:          rookCephv1.Labels{defaults.ODFResourceProfileKey: sc.Spec.ResourceProfile},
				rookCephv1.KeyOSD:          rookCephv1.Labels{defaults.ODFResourceProfileKey: sc.Spec.ResourceProfile},
				rookCephv1.KeyMonitoring:   getCephClusterMonitoringLabels(*sc),
				rookCephv1.KeyCephExporter: getCephClusterMonitoringLabels(*sc),
			},
			CSI: rookCephv1.CSIDriverSpec{
				ReadAffinity: util.GetReadAffinityOptions(sc),
				CephFS: rookCephv1.CSICephFSSpec{
					KernelMountOptions: util.GetCephFSKernelMountOptions(sc),
				},
			},
			SkipUpgradeChecks:            sc.Spec.ManagedResources.CephCluster.SkipUpgradeChecks,
			UpgradeOSDRequiresHealthyPGs: sc.Spec.ManagedResources.CephCluster.UpgradeOSDRequiresHealthyPGs,
			// if resource profile change is in progress, then set this flag to false
			ContinueUpgradeAfterChecksEvenIfNotHealthy: sc.Spec.ResourceProfile == sc.Status.LastAppliedResourceProfile,
		},
	}

	if sc.Spec.ManagedResources.CephCluster.ContinueUpgradeAfterChecksEvenIfNotHealthy != nil {
		cephCluster.Spec.ContinueUpgradeAfterChecksEvenIfNotHealthy = *sc.Spec.ManagedResources.CephCluster.ContinueUpgradeAfterChecksEvenIfNotHealthy
	}

	if sc.Spec.ManagedResources.CephCluster.WaitTimeoutForHealthyOSDInMinutes != 0 {
		cephCluster.Spec.WaitTimeoutForHealthyOSDInMinutes = sc.Spec.ManagedResources.CephCluster.WaitTimeoutForHealthyOSDInMinutes
	}

	if sc.Spec.ManagedResources.CephCluster.OsdMaintenanceTimeout != 0 {
		cephCluster.Spec.DisruptionManagement.OSDMaintenanceTimeout = sc.Spec.ManagedResources.CephCluster.OsdMaintenanceTimeout
	}

	if sc.Spec.ManagedResources.CephCluster.HealthCheck != nil {
		cephCluster.Spec.HealthCheck = *sc.Spec.ManagedResources.CephCluster.HealthCheck
	}

	if sc.Spec.LogCollector != nil {
		if sc.Spec.LogCollector.Periodicity != "" {
			cephCluster.Spec.LogCollector.Periodicity = sc.Spec.LogCollector.Periodicity
		}
		if sc.Spec.LogCollector.MaxLogSize != nil {
			cephCluster.Spec.LogCollector.MaxLogSize = sc.Spec.LogCollector.MaxLogSize
		}
	}

	monPVCTemplate := sc.Spec.MonPVCTemplate
	monDataDirHostPath := sc.Spec.MonDataDirHostPath
	// If the `monPVCTemplate` is provided, the mons will provisioned on the
	// provided `monPVCTemplate`.
	if monPVCTemplate != nil {
		cephCluster.Spec.Mon.VolumeClaimTemplate = &rookCephv1.VolumeClaimTemplate{
			ObjectMeta: monPVCTemplate.ObjectMeta,
			Spec:       monPVCTemplate.Spec,
		}
		// If the `monDataDirHostPath` is provided without the `monPVCTemplate`,
		// the mons will be provisioned on the provided `monDataDirHostPath`.
	} else if len(monDataDirHostPath) > 0 {
		cephCluster.Spec.DataDirHostPath = monDataDirHostPath
		// If no `monPVCTemplate` and `monDataDirHostPath` is provided, the mons will
		// be provisioned using the PVC template of first StorageDeviceSets if present.
	} else if len(sc.Spec.StorageDeviceSets) > 0 {
		ds := sc.Spec.StorageDeviceSets[0]
		cephCluster.Spec.Mon.VolumeClaimTemplate = &rookCephv1.VolumeClaimTemplate{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ds.DataPVCTemplate.Spec.StorageClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: resource.MustParse("50Gi"),
					},
				},
			},
		}
	} else {
		reqLogger.Info("No monDataDirHostPath, monPVCTemplate or storageDeviceSets configured for StorageCluster.", "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
	}

	// if kmsConfig is not 'nil', add the KMS details to CephCluster spec
	if kmsConfigMap != nil {
		// Set default KMS_PROVIDER. Possible values are: vault, ibmkeyprotect, kmip.
		if _, ok := kmsConfigMap.Data["KMS_PROVIDER"]; !ok {
			kmsConfigMap.Data["KMS_PROVIDER"] = VaultKMSProvider
		}
		var kmsProviderName = kmsConfigMap.Data["KMS_PROVIDER"]
		// vault as a KMS service provider
		if kmsProviderName == VaultKMSProvider {
			// Set default VAULT_SECRET_ENGINE values
			if _, ok := kmsConfigMap.Data["VAULT_SECRET_ENGINE"]; !ok {
				kmsConfigMap.Data["VAULT_SECRET_ENGINE"] = "kv"
			}
			// Set default VAULT_AUTH_METHOD. Possible values are: token, kubernetes.
			if _, ok := kmsConfigMap.Data["VAULT_AUTH_METHOD"]; !ok {
				kmsConfigMap.Data["VAULT_AUTH_METHOD"] = VaultTokenAuthMethod
			}
			// Set TokenSecretName only for vault token based auth method
			if kmsConfigMap.Data["VAULT_AUTH_METHOD"] == VaultTokenAuthMethod {
				// Secret is created by UI in "openshift-storage" namespace
				cephCluster.Spec.Security.KeyManagementService.TokenSecretName = KMSTokenSecretName
			}
		} else if kmsProviderName == IbmKeyProtectKMSProvider || kmsProviderName == ThalesKMSProvider {
			// Secret is created by UI in "openshift-storage" namespace
			cephCluster.Spec.Security.KeyManagementService.TokenSecretName = kmsConfigMap.Data[kmsProviderSecretKeyMap[kmsProviderName]]
		}
		cephCluster.Spec.Security.KeyManagementService.ConnectionDetails = kmsConfigMap.Data
	}

	isEnabled, rotationSchedule := statusutil.GetKeyRotationSpec(sc)
	cephCluster.Spec.Security.KeyRotation.Enabled = isEnabled
	cephCluster.Spec.Security.KeyRotation.Schedule = rotationSchedule

	return cephCluster
}

func isMultus(nwSpec *rookCephv1.NetworkSpec) bool {
	if nwSpec != nil {
		return nwSpec.IsMultus()
	}
	return false
}

func validateMultusSelectors(selectors map[rookCephv1.CephNetworkType]string) error {
	publicNetwork, validPublicNetworkKey := selectors[publicNetworkSelectorKey]
	clusterNetwork, validClusterNetworkKey := selectors[clusterNetworkSelectorKey]
	if !validPublicNetworkKey && !validClusterNetworkKey {
		return fmt.Errorf("invalid value of the keys for the network selectors. keys should be public and cluster only")
	}
	if publicNetwork == "" && clusterNetwork == "" {
		return fmt.Errorf("both public and cluster network selector values can't be empty")
	}
	return nil
}

func getNetworkSpec(sc ocsv1.StorageCluster) rookCephv1.NetworkSpec {
	networkSpec := rookCephv1.NetworkSpec{}
	if sc.Spec.Network != nil {
		networkSpec = *sc.Spec.Network
	}
	// respect both the old way and the new way for enabling HostNetwork
	networkSpec.HostNetwork = networkSpec.HostNetwork || sc.Spec.HostNetwork

	if networkSpec.Connections == nil {
		networkSpec.Connections = &rookCephv1.ConnectionsSpec{}
	}
	// Always require msgr2 for all modes of ODF
	networkSpec.Connections.RequireMsgr2 = true

	return networkSpec
}

func newExternalCephCluster(sc *ocsv1.StorageCluster, monitoringIP, monitoringPort string) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	maxLogSize := resource.MustParse("500Mi")

	logCollector := rookCephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	var monitoringSpec = rookCephv1.MonitoringSpec{Enabled: false}

	if monitoringIP != "" {
		monitoringSpec.Enabled = true
		// replace any comma with space and collect all the non-empty items
		monIPArr := parseMonitoringIPs(monitoringIP)
		monitoringSpec.ExternalMgrEndpoints = make([]corev1.EndpointAddress, len(monIPArr))
		for idx, eachMonIP := range monIPArr {
			monitoringSpec.ExternalMgrEndpoints[idx].IP = eachMonIP
		}
		if monitoringPort != "" {
			if uint16Val, err := strconv.ParseUint(monitoringPort, 10, 16); err == nil {
				monitoringSpec.ExternalMgrPrometheusPort = uint16(uint16Val)
			}
		}
	}

	externalCephCluster := &rookCephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephCluster(sc),
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: rookCephv1.ClusterSpec{
			External: rookCephv1.ExternalSpec{
				Enable: true,
			},
			CrashCollector: rookCephv1.CrashCollectorSpec{
				Disable: true,
			},
			DisruptionManagement: rookCephv1.DisruptionManagementSpec{
				ManagePodBudgets:               false,
				ManageMachineDisruptionBudgets: false,
			},
			Monitoring: monitoringSpec,
			Network:    getNetworkSpec(*sc),
			Labels: rookCephv1.LabelsSpec{
				rookCephv1.KeyMonitoring:   getCephClusterMonitoringLabels(*sc),
				rookCephv1.KeyCephExporter: getCephClusterMonitoringLabels(*sc),
			},
			LogCollector: logCollector,
			CSI: rookCephv1.CSIDriverSpec{
				ReadAffinity: util.GetReadAffinityOptions(sc),
				CephFS: rookCephv1.CSICephFSSpec{
					KernelMountOptions: util.GetCephFSKernelMountOptions(sc),
				},
			},
		},
	}

	return externalCephCluster
}

func getMinDeviceSetReplica(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterModeDeviceSetReplica
	}
	if statusutil.IsSingleNodeDeployment() {
		return 1
	}
	return defaults.DeviceSetReplica
}

func getReplicasPerFailureDomain(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterReplicasPerFailureDomain
	}
	return defaults.ReplicasPerFailureDomain
}

// getCephPoolReplicatedSize returns the default replica per cluster count for a
// StorageCluster type
func getCephPoolReplicatedSize(sc *ocsv1.StorageCluster) uint {
	if arbiterEnabled(sc) {
		return uint(4)
	}
	return uint(3)
}

// getMinimumNodes returns the minimum number of nodes that are required for the Storage Cluster of various configurations
func getMinimumNodes(sc *ocsv1.StorageCluster) int {
	// Case 1: When replicasPerFailureDomain is 1.
	// A node is the smallest failure domain that is possible. We definitely
	// want the devices in the same device set to be in different failure
	// domains which also means different nodes. In this case, minimum number of
	// nodes is getMinDeviceSetReplica() * 1.

	// Case 2: When replicasPerFailureDomain is greater than 1
	// In certain scenarios, it may be valid to place one or more replicas
	// within the failure domain on the same node but it does not make much
	// sense. It provides protection only against disk failures and not node
	// failures.
	// For example:
	// a. FailureDomain=node, replicasPerFailureDomain>1
	// b. FailureDomain=rack, numNodesInTheRack=2, replicasPerFailureDomain>2
	// c. FailureDomain=zone, numNodesInTheZone=2, replicasPerFailureDomain>2
	// Therefore, we make an assumption that the replicas must be placed on
	// different nodes. This logic gets us to the equation below. If the user
	// needs to override this assumption, we can provide a flag (like
	// allowReplicasOnSameNode) in the future.

	maxReplica := getMinDeviceSetReplica(sc) * getReplicasPerFailureDomain(sc)

	for _, deviceSet := range sc.Spec.StorageDeviceSets {
		if deviceSet.Replica > maxReplica {
			maxReplica = deviceSet.Replica
		}
	}

	return maxReplica
}

func getMgrCount(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterModeMgrCount
	}
	mgrCount := sc.Spec.ManagedResources.CephCluster.MgrCount
	if mgrCount != 0 {
		return mgrCount
	}
	if statusutil.IsSingleNodeDeployment() {
		return 1
	}
	return defaults.DefaultMgrCount
}

func getMonCount(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterModeMonCount
	}
	monCount := sc.Spec.ManagedResources.CephCluster.MonCount
	if monCount != 0 {
		return monCount
	}
	return defaults.DefaultMonCount
}

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(sc *ocsv1.StorageCluster) []rookCephv1.StorageClassDeviceSet {
	storageDeviceSets := sc.Spec.StorageDeviceSets
	topologyMap := sc.Status.NodeTopologies

	var storageClassDeviceSets []rookCephv1.StorageClassDeviceSet

	for _, ds := range storageDeviceSets {
		resources := ds.Resources
		if resources.Requests == nil && resources.Limits == nil {
			resources = defaults.GetProfileDaemonResources("osd", sc)
		}

		portable := ds.Portable

		topologyKey := ds.TopologyKey
		topologyKeyValues := []string{}

		noPlacementTsc := ds.Placement.TopologySpreadConstraints == nil
		noPlacement := ds.Placement.NodeAffinity == nil && ds.Placement.PodAffinity == nil && ds.Placement.PodAntiAffinity == nil && noPlacementTsc
		noPreparePlacementTsc := ds.PreparePlacement.TopologySpreadConstraints == nil
		noPreparePlacement := ds.PreparePlacement.NodeAffinity == nil && ds.PreparePlacement.PodAffinity == nil && ds.PreparePlacement.PodAntiAffinity == nil && noPreparePlacementTsc

		if topologyKey == "" {
			topologyKey = getFailureDomain(sc)
		}

		if topologyKey == "host" {
			portable = false
		}

		if topologyMap != nil {
			topologyKey, topologyKeyValues = topologyMap.GetKeyValues(topologyKey)
		}

		count, replica := countAndReplicaOf(&ds)
		for i := 0; i < replica; i++ {
			placement := rookCephv1.Placement{}
			preparePlacement := rookCephv1.Placement{}

			if noPlacement {
				in := getPlacement(sc, "osd")
				(&in).DeepCopyInto(&placement)

				if noPreparePlacement {
					in := getPlacement(sc, "osd-prepare")
					(&in).DeepCopyInto(&preparePlacement)
				} else {
					preparePlacement = ds.PreparePlacement
				}
			} else if !noPlacement && noPreparePlacement {
				preparePlacement = ds.Placement
				placement = ds.Placement
			} else {
				preparePlacement = ds.PreparePlacement
				placement = ds.Placement
			}

			// Add default TSCs if not set to ensure even distribution of OSDs across nodes
			if len(placement.TopologySpreadConstraints) == 0 {
				placement.TopologySpreadConstraints = append(placement.TopologySpreadConstraints, defaults.DaemonPlacements["osd"].TopologySpreadConstraints...)
			}
			if len(preparePlacement.TopologySpreadConstraints) == 0 {
				preparePlacement.TopologySpreadConstraints = append(preparePlacement.TopologySpreadConstraints, defaults.DaemonPlacements["osd-prepare"].TopologySpreadConstraints...)
			}

			if len(topologyKeyValues) >= getMinDeviceSetReplica(sc) {
				// Hard constraints are set in OSD placement for portable volumes with rack failure domain
				// domain as there is no node affinity in PVs. This restricts the movement of OSDs
				// between failure domain.
				if noPlacementTsc && portable && !strings.Contains(topologyKey, "zone") {
					addStrictFailureDomainTSC(&placement, topologyKey)
				}
				// If topologyKey is not host, append additional topology spread constraint to the
				// default preparePlacement. This serves even distribution at the host level
				// within a failure domain (zone/rack).
				if noPlacementTsc && noPreparePlacementTsc {
					if topologyKey != corev1.LabelHostname {
						addStrictFailureDomainTSC(&preparePlacement, topologyKey)
					} else {
						preparePlacement.TopologySpreadConstraints[0].TopologyKey = topologyKey
					}
				}
			}

			// Annotation crushDeviceClass ensures osd with different CRUSH device class than the one detected by Ceph
			crushDeviceClass := ds.DeviceType
			if ds.DeviceClass != "" {
				crushDeviceClass = ds.DeviceClass
			}

			annotations := map[string]string{
				"crushDeviceClass": crushDeviceClass,
			}
			// Annotation crushInitialWeight is an optional, explicit weight to set upon OSD's init (as float, in TiB units).
			// ROOK & Ceph do not want any (optional) Ti[B] suffix, so trim it here.
			// If not set, Ceph will define OSD's weight based on its capacity.
			crushInitialWeight := strings.TrimSuffix(strings.TrimSuffix(ds.InitialWeight, "Ti"), "TiB")
			if crushInitialWeight != "" {
				annotations["crushInitialWeight"] = crushInitialWeight
			}
			// Annotation crushPrimaryAffinity is an optional, explicit primary-affinity value within the range [0,1) to
			// set upon OSD's deployment. If not set, Ceph sets default value to 1
			if ds.PrimaryAffinity != "" {
				annotations["crushPrimaryAffinity"] = ds.PrimaryAffinity
			}
			ds.DataPVCTemplate.Annotations = annotations

			set := rookCephv1.StorageClassDeviceSet{
				Name:             fmt.Sprintf("%s-%d", ds.Name, i),
				Count:            count,
				Resources:        resources,
				Placement:        placement,
				PreparePlacement: &preparePlacement,
				Config:           ds.Config.ToMap(),
				VolumeClaimTemplates: []rookCephv1.VolumeClaimTemplate{{
					ObjectMeta: ds.DataPVCTemplate.ObjectMeta,
					Spec:       ds.DataPVCTemplate.Spec,
				}},
				Portable:            portable,
				TuneSlowDeviceClass: ds.Config.TuneSlowDeviceClass,
				TuneFastDeviceClass: ds.Config.TuneFastDeviceClass,
				Encrypted:           isDeviceSetToBeEncrypted(sc, ds),
			}

			if ds.MetadataPVCTemplate != nil {
				ds.MetadataPVCTemplate.ObjectMeta.Name = metadataPVCName
				set.VolumeClaimTemplates = append(set.VolumeClaimTemplates, rookCephv1.VolumeClaimTemplate{
					ObjectMeta: ds.MetadataPVCTemplate.ObjectMeta,
					Spec:       ds.MetadataPVCTemplate.Spec,
				})
			}
			if ds.WalPVCTemplate != nil {
				ds.WalPVCTemplate.ObjectMeta.Name = walPVCName
				set.VolumeClaimTemplates = append(set.VolumeClaimTemplates, rookCephv1.VolumeClaimTemplate{
					ObjectMeta: ds.WalPVCTemplate.ObjectMeta,
					Spec:       ds.WalPVCTemplate.Spec,
				})
			}

			storageClassDeviceSets = append(storageClassDeviceSets, set)
		}
	}

	if sc.Spec.ManagedResources.CephNonResilientPools.Enable {
		// Creating osds for non-resilient pools
		for _, failureDomainValue := range sc.Status.FailureDomainValues {
			ds := rookCephv1.StorageClassDeviceSet{}
			ds.Name = failureDomainValue
			ds.Count = sc.Spec.ManagedResources.CephNonResilientPools.Count
			ds.Resources = sc.Spec.ManagedResources.CephNonResilientPools.Resources
			if ds.Resources.Requests == nil && ds.Resources.Limits == nil {
				ds.Resources = defaults.GetProfileDaemonResources("osd", sc)
			}
			// passing on existing defaults from existing devcicesets
			ds.TuneSlowDeviceClass = sc.Spec.StorageDeviceSets[0].Config.TuneSlowDeviceClass
			ds.TuneFastDeviceClass = sc.Spec.StorageDeviceSets[0].Config.TuneFastDeviceClass
			annotations := map[string]string{
				"crushDeviceClass": failureDomainValue,
			}
			if !reflect.DeepEqual(sc.Spec.ManagedResources.CephNonResilientPools.VolumeClaimTemplate, corev1.PersistentVolumeClaim{}) {
				ds.VolumeClaimTemplates = []rookCephv1.VolumeClaimTemplate{{
					ObjectMeta: sc.Spec.ManagedResources.CephNonResilientPools.VolumeClaimTemplate.ObjectMeta,
					Spec:       sc.Spec.ManagedResources.CephNonResilientPools.VolumeClaimTemplate.Spec,
				}}
			} else {
				// If not defined use the spec for volumeclaimtemplate from existing devicesets
				ds.VolumeClaimTemplates = []rookCephv1.VolumeClaimTemplate{{
					ObjectMeta: sc.Spec.StorageDeviceSets[0].DataPVCTemplate.ObjectMeta,
					Spec:       sc.Spec.StorageDeviceSets[0].DataPVCTemplate.Spec,
				}}
			}
			ds.VolumeClaimTemplates[0].Annotations = annotations
			ds.Portable = sc.Status.FailureDomain != "host"
			placement := rookCephv1.Placement{}
			// Portable OSDs must have an node affinity to their zone
			// Non-portable OSDs must have an affinity to the node
			placement.NodeAffinity = &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
					NodeSelectorTerms: []corev1.NodeSelectorTerm{
						{
							MatchExpressions: []corev1.NodeSelectorRequirement{
								{
									Key:      sc.Status.FailureDomainKey,
									Operator: corev1.NodeSelectorOpIn,
									Values:   []string{failureDomainValue},
								},
							},
						},
					},
				},
			}
			// default tolerations for any ocs operator pods
			placement.Tolerations = []corev1.Toleration{
				{
					Key:      defaults.NodeTolerationKey,
					Operator: corev1.TolerationOpEqual,
					Value:    "true",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			}
			ds.Placement = placement
			ds.PreparePlacement = &placement
			storageClassDeviceSets = append(storageClassDeviceSets, ds)
		}
	}
	return storageClassDeviceSets
}

func countAndReplicaOf(ds *ocsv1.StorageDeviceSet) (int, int) {
	count := ds.Count
	replica := ds.Replica
	if replica == 0 {
		replica = defaults.DeviceSetReplica

		// This is a temporary hack in place due to limitations
		// in the current implementation of the OCP console.
		// The console is hardcoded to create a StorageCluster
		// with a Count of 3, as made sense for the previous
		// behavior, but it cannot be updated until the next
		// z-stream release of OCP 4.2. This workaround is to
		// enable the new behavior while the console is waiting
		// to be updated.
		// TODO: Remove this behavior when OCP console is updated
		count = count / 3
	}
	return count, replica
}

func isDeviceSetToBeEncrypted(sc *ocsv1.StorageCluster, ds ocsv1.StorageDeviceSet) bool {
	if ds.Encrypted != nil {
		return *ds.Encrypted
	}
	return sc.Spec.Encryption.Enable || sc.Spec.Encryption.ClusterWide
}

func newCephDaemonResources(sc *ocsv1.StorageCluster) map[string]corev1.ResourceRequirements {
	resources := map[string]corev1.ResourceRequirements{
		"mon": defaults.GetProfileDaemonResources("mon", sc),
		"mgr": defaults.GetProfileDaemonResources("mgr", sc),
	}
	custom := sc.Spec.Resources
	for k := range custom {
		if r, ok := custom[k]; ok {
			resources[k] = r
		}
	}

	return resources
}

// The checkTuneStorageDevices function checks whether devices from the given
// storage class are a known type that should expclitly be tuned for fast or
// slow access.
func (r *StorageClusterReconciler) checkTuneStorageDevices(ds ocsv1.StorageDeviceSet) (diskSpeed, error) {
	deviceType := ds.DeviceType

	if DeviceTypeHDD == strings.ToLower(deviceType) {
		return diskSpeedSlow, nil
	}

	if DeviceTypeSSD == strings.ToLower(deviceType) || DeviceTypeNVMe == strings.ToLower(deviceType) {
		return diskSpeedFast, nil
	}

	storageClassName := *ds.DataPVCTemplate.Spec.StorageClassName
	storageClass := &storagev1.StorageClass{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: storageClassName}, storageClass)
	if err != nil {
		return diskSpeedUnknown, fmt.Errorf("failed to retrieve StorageClass %q. %+v", storageClassName, err)
	}

	for _, dt := range knownDiskTypes {
		if string(dt.provisioner) != storageClass.Provisioner {
			continue
		}

		if dt.storageClassType != storageClass.Parameters["type"] {
			continue
		}

		return dt.speed, nil
	}

	tuneFastDevices, err := platform.DevicesDefaultToFastForThisPlatform()
	if err != nil {
		return diskSpeedUnknown, err
	}
	if tuneFastDevices {
		return diskSpeedFast, nil
	}

	// not a known disk type, don't tune
	return diskSpeedUnknown, nil
}

func allowUnsupportedCephVersion() bool {
	return defaults.IsUnsupportedCephVersionAllowed == "allowed"
}

func generateStretchClusterSpec(sc *ocsv1.StorageCluster) *rookCephv1.StretchClusterSpec {
	var zones []string
	stretchClusterSpec := rookCephv1.StretchClusterSpec{}
	stretchClusterSpec.FailureDomainLabel, zones = sc.Status.NodeTopologies.GetKeyValues(getFailureDomain(sc))

	for _, zone := range zones {
		if zone == sc.Spec.NodeTopologies.ArbiterLocation {
			continue
		}
		stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, rookCephv1.MonZoneSpec{
			Name:    zone,
			Arbiter: false,
		})
	}

	arbiterZoneSpec := rookCephv1.MonZoneSpec{
		Name:    sc.Spec.NodeTopologies.ArbiterLocation,
		Arbiter: true,
	}
	if sc.Spec.Arbiter.ArbiterMonPVCTemplate != nil {
		arbiterZoneSpec.VolumeClaimTemplate = &rookCephv1.VolumeClaimTemplate{
			ObjectMeta: sc.Spec.Arbiter.ArbiterMonPVCTemplate.ObjectMeta,
			Spec:       sc.Spec.Arbiter.ArbiterMonPVCTemplate.Spec,
		}
	}
	stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, arbiterZoneSpec)

	return &stretchClusterSpec
}

func generateMonSpec(sc *ocsv1.StorageCluster) rookCephv1.MonSpec {
	spec := rookCephv1.MonSpec{
		Count:                getMonCount(sc),
		AllowMultiplePerNode: statusutil.IsSingleNodeDeployment(),
	}
	if arbiterEnabled(sc) {
		spec.StretchCluster = generateStretchClusterSpec(sc)
	}
	return spec
}

func generateMgrSpec(sc *ocsv1.StorageCluster) rookCephv1.MgrSpec {
	spec := rookCephv1.MgrSpec{
		Count:                getMgrCount(sc),
		AllowMultiplePerNode: statusutil.IsSingleNodeDeployment(),
		Modules: []rookCephv1.Module{
			{Name: "pg_autoscaler", Enabled: true},
			{Name: "balancer", Enabled: true},
			{Name: "rook", Enabled: false},
		},
	}

	return spec
}

// addStrictFailureDomainTSC adds hard topology constraints at failure domain level
// and uses soft topology constraints within failure domain (across host).
func addStrictFailureDomainTSC(placement *rookCephv1.Placement, topologyKey string) {
	newTSC := placement.TopologySpreadConstraints[0]
	newTSC.TopologyKey = topologyKey
	newTSC.WhenUnsatisfiable = "DoNotSchedule"

	placement.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{newTSC, placement.TopologySpreadConstraints[0]}
}

// ensureCreated ensures that cephFilesystem resources exist in the desired
// state.
func createPrometheusRules(r *StorageClusterReconciler, sc *ocsv1.StorageCluster, cluster *rookCephv1.CephCluster) error {
	if !cluster.Spec.Monitoring.Enabled {
		r.Log.Info("prometheus rules skipped", "CephCluster", klog.KRef(cluster.Namespace, cluster.Name))
		return nil
	}
	if testSkipPrometheusRules {
		r.Log.Info("skipping prometheus rules in test")
		return nil
	}

	rules := localPrometheusRules
	name := prometheusLocalRuleName
	if cluster.Spec.External.Enable {
		rules = externalPrometheusRules
		name = prometheusExternalRuleName
	}
	prometheusRule, err := parsePrometheusRule(rules)
	if err != nil {
		r.Log.Error(err, "Unable to retrieve prometheus rules.", "CephCluster", klog.KRef(cluster.Namespace, cluster.Name))
		return err
	}
	prometheusRule.SetName(name)
	prometheusRule.SetNamespace(sc.Namespace)
	if err := controllerutil.SetControllerReference(sc, prometheusRule, r.Scheme); err != nil {
		r.Log.Error(err, "Unable to set controller reference for prometheus rules.", "CephCluster")
		return err
	}
	applyLabels(getCephClusterMonitoringLabels(*sc), &prometheusRule.ObjectMeta)

	replaceTokens := []replaceToken{
		{
			recordOrAlertName: "CephMgrIsAbsent",
			wordToReplace:     "openshift-storage",
			replaceWith:       sc.Namespace,
		},
	}

	// if nearFullRatio/backfillFullRatio/fullRatio are specified on the StorageCLuster CR, replace the values in the prometheus rule accordingly
	specifiedNearFullRatio := sc.Spec.ManagedResources.CephCluster.NearFullRatio
	specifiedBackfillFullRatio := sc.Spec.ManagedResources.CephCluster.BackfillFullRatio
	specifiedFullRatio := sc.Spec.ManagedResources.CephCluster.FullRatio

	if specifiedNearFullRatio != nil {
		replaceTokens = append(replaceTokens,
			createReplaceToken("", "", "75%", fmt.Sprintf("%.2f%%", *specifiedNearFullRatio*100)),
			createReplaceToken("", "", "0.75", fmt.Sprintf("%f", *specifiedNearFullRatio)))
	}
	if specifiedBackfillFullRatio != nil {
		replaceTokens = append(replaceTokens,
			createReplaceToken("", "", "80%", fmt.Sprintf("%.2f%%", *specifiedBackfillFullRatio*100)),
			createReplaceToken("", "", "0.80", fmt.Sprintf("%f", *specifiedBackfillFullRatio)))
	}
	if specifiedFullRatio != nil {
		replaceTokens = append(replaceTokens,
			createReplaceToken("", "", "85%", fmt.Sprintf("%.2f%%", *specifiedFullRatio*100)),
			createReplaceToken("", "", "0.85", fmt.Sprintf("%f", *specifiedFullRatio)))
	}

	// nothing to replace in external mode
	if name != prometheusExternalRuleName {
		changePromRule(prometheusRule, replaceTokens)
	}

	if err := createOrUpdatePrometheusRule(r, prometheusRule); err != nil {
		r.Log.Error(err, "Prometheus rules could not be created.", "CephCluster", klog.KRef(cluster.Namespace, cluster.Name))
		return err
	}

	r.Log.Info("prometheus rules deployed", "CephCluster", klog.KRef(cluster.Namespace, cluster.Name))

	return nil
}

// applyLabels adds labels to object meta, overwriting keys that are already defined.
func applyLabels(labels map[string]string, t *metav1.ObjectMeta) {
	if t.Labels == nil {
		t.Labels = map[string]string{}
	}
	for k, v := range labels {
		t.Labels[k] = v
	}
}

type replaceToken struct {
	groupName         string
	recordOrAlertName string
	wordToReplace     string
	replaceWith       string
}

func createReplaceToken(groupName, recordOrAlertName, wordToReplace, replaceWith string) replaceToken {
	return replaceToken{
		groupName:         groupName,
		recordOrAlertName: recordOrAlertName,
		wordToReplace:     wordToReplace,
		replaceWith:       replaceWith,
	}
}

// changePromRule replaces the wordToReplace with replaceWith in the PrometheusRule
// This can be used to update the values in the PrometheusRule dynamically
func changePromRule(promRule *monitoringv1.PrometheusRule, tokens []replaceToken) {
	if promRule == nil {
		return
	}

	// Iterate over each token for replacements
	for _, token := range tokens {
		// Skip if the word and replacement are the same
		if token.replaceWith == token.wordToReplace {
			continue
		}

		// Iterate through all groups in the Prometheus rule
		for groupIdx := range promRule.Spec.Groups {
			group := &promRule.Spec.Groups[groupIdx]
			// If groupName is specified, ensure it matches; otherwise, apply to all groups
			if token.groupName != "" && token.groupName != group.Name {
				continue
			}

			// Iterate through the rules in the group
			for ruleIdx := range group.Rules {
				rule := &group.Rules[ruleIdx]
				// If recordOrAlertName is specified, ensure it matches; otherwise, apply to all rules
				if token.recordOrAlertName == "" || rule.Record == token.recordOrAlertName || rule.Alert == token.recordOrAlertName {
					// Update the annotations in the rule
					if rule.Annotations != nil {
						// Update description if it exists
						if description, exists := rule.Annotations["description"]; exists {
							newDescription := strings.Replace(description, token.wordToReplace, token.replaceWith, -1)
							rule.Annotations["description"] = newDescription
						}
					}
					// Update the expression field in the rule
					exprStr := rule.Expr.String()
					if exprStr != "" {
						newExpr := strings.Replace(exprStr, token.wordToReplace, token.replaceWith, -1)
						rule.Expr = intstr.Parse(newExpr)
					}
				}
			}
		}
	}
}

// parsePrometheusRule returns provided prometheus rules or an error
func parsePrometheusRule(rules string) (*monitoringv1.PrometheusRule, error) {
	var rule monitoringv1.PrometheusRule
	err := k8sYAML.NewYAMLOrJSONDecoder(bytes.NewBufferString(string(rules)), 1000).Decode(&rule)
	if err != nil {
		return nil, fmt.Errorf("prometheusRules could not be decoded. %v", err)
	}
	return &rule, nil
}

// createOrUpdatePrometheusRule creates a prometheusRule object or an error
func createOrUpdatePrometheusRule(r *StorageClusterReconciler, prometheusRule *monitoringv1.PrometheusRule) error {
	name := prometheusRule.GetName()
	namespace := prometheusRule.GetNamespace()
	client, err := getMonitoringClient()
	if err != nil {
		return fmt.Errorf("failed to get monitoring client. %v", err)
	}
	_, err = client.MonitoringV1().PrometheusRules(namespace).Create(context.TODO(), prometheusRule, metav1.CreateOptions{})
	if err != nil {
		if !errors.IsAlreadyExists(err) {
			return fmt.Errorf("failed to create prometheusRules. %v", err)
		}
		// Get current PrometheusRule so the ResourceVersion can be set as needed
		// for the object update operation
		promRule, err := client.MonitoringV1().PrometheusRules(namespace).Get(context.TODO(), name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("failed to get prometheusRule object. %v", err)
		}
		promRule.Spec = prometheusRule.Spec
		promRule.ObjectMeta.Labels = prometheusRule.ObjectMeta.Labels
		_, err = client.MonitoringV1().PrometheusRules(namespace).Update(context.TODO(), promRule, metav1.UpdateOptions{})
		if err != nil {
			r.Log.Error(err, "failed to update prometheus rules.", "CephCluster")
			return err
		}
	}
	return nil
}

func getMonitoringClient() (*monitoringclient.Clientset, error) {
	cfg, err := clientcmd.BuildConfigFromFlags("", "")
	if err != nil {
		return nil, fmt.Errorf("failed to build config foo. %v", err)
	}
	client, err := monitoringclient.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to get monitoring client bar. %v", err)
	}
	return client, nil
}

// getIPFamilyConfig checks for a Single Stack IPv6 or a Dual Stack cluster
func getIPFamilyConfig(c client.Client) (rookCephv1.IPFamilyType, bool, error) {
	isIPv6 := false
	isIPv4 := false
	networkConfig := &configv1.Network{}
	err := c.Get(context.TODO(), types.NamespacedName{Name: "cluster", Namespace: ""}, networkConfig)
	if err != nil {
		return "", false, fmt.Errorf("could not get network config details. %v", err)
	}

	for _, cidr := range networkConfig.Status.ClusterNetwork {
		if strings.Count(cidr.CIDR, ":") < 2 {
			isIPv4 = true
		} else if strings.Count(cidr.CIDR, ":") >= 2 {
			isIPv6 = true
		}
	}

	if isIPv4 && isIPv6 {
		return "", true, nil
	} else if isIPv6 { // IPv6 single stack cluster
		return rookCephv1.IPv6, false, nil
	}

	return rookCephv1.IPv4, false, nil
}

func updateOSDStore(existingOSDStore rookCephv1.OSDStore) rookCephv1.OSDStore {
	if existingOSDStore.Type == string(rookCephv1.StoreTypeBlueStoreRDR) {
		return rookCephv1.OSDStore{
			Type:        string(rookCephv1.StoreTypeBlueStore),
			UpdateStore: "yes-really-update-store",
		}
	}
	return existingOSDStore
}

func getOsdCount(sc *ocsv1.StorageCluster) int {
	storageClassDeviceSets := newStorageClassDeviceSets(sc)
	osdCount := 0
	for _, ds := range storageClassDeviceSets {
		osdCount += ds.Count
	}
	return osdCount
}

func determineDefaultCephDeviceClass(foundDeviceClasses []rookCephv1.DeviceClasses, isReplica1 bool, replica1DeviceClasses []string) string {
	// If device classes are found in status
	if len(foundDeviceClasses) != 0 {
		// If replica-1 is not enabled return the first device class
		if !isReplica1 {
			return foundDeviceClasses[0].Name
		}
		// If replica-1 is enabled return the first one that is not a replica1 device class
		if len(foundDeviceClasses) != 0 {
			replica1DeviceClassMap := make(map[string]bool)
			for _, deviceClass := range replica1DeviceClasses {
				replica1DeviceClassMap[deviceClass] = true
			}
			for _, deviceClass := range foundDeviceClasses {
				if found := replica1DeviceClassMap[deviceClass.Name]; !found {
					return deviceClass.Name
				}
			}
		}
	}
	// if no device classes are found in status return empty string
	return ""
}

// isEncrptionSettingUpdated checks whether encryption was enabled or disabled by comparing the clusterWide encryption setting
// of the new storageCluster CR with the encryption setting on the StorageClassDeviceSets of the existing cephCluster CR.
func isEncrptionSettingUpdated(clusterWideEncrytion bool, existingDeviceSet []rookCephv1.StorageClassDeviceSet) bool {
	for i := range existingDeviceSet {
		if existingDeviceSet[i].Encrypted != clusterWideEncrytion {
			return true
		}
	}
	return false
}

// setDefaultMetadataPoolSpec sets the common pool spec for all metadata pools as necessary
func setDefaultMetadataPoolSpec(poolSpec *rookCephv1.PoolSpec, sc *ocsv1.StorageCluster) {
	poolSpec.EnableCrushUpdates = true
	if poolSpec.DeviceClass == "" {
		poolSpec.DeviceClass = sc.Status.DefaultCephDeviceClass
	}
	if poolSpec.FailureDomain == "" {
		poolSpec.FailureDomain = getFailureDomain(sc)
	}
	// Set default replication settings if necessary
	// Always set the default Size & ReplicasPerFailureDomain in arbiter mode
	defaultReplicatedSpec := generateCephReplicatedSpec(sc, poolTypeMetadata)
	if poolSpec.Replicated.Size == 0 || arbiterEnabled(sc) {
		poolSpec.Replicated.Size = defaultReplicatedSpec.Size
	}
	if poolSpec.Replicated.ReplicasPerFailureDomain == 0 || arbiterEnabled(sc) {
		poolSpec.Replicated.ReplicasPerFailureDomain = defaultReplicatedSpec.ReplicasPerFailureDomain
	}
}

// setDefaultDataPoolSpec sets the common pool spec for all data pools as necessary
func setDefaultDataPoolSpec(poolSpec *rookCephv1.PoolSpec, sc *ocsv1.StorageCluster) {
	poolSpec.EnableCrushUpdates = true
	if poolSpec.DeviceClass == "" {
		poolSpec.DeviceClass = sc.Status.DefaultCephDeviceClass
	}
	if poolSpec.FailureDomain == "" {
		poolSpec.FailureDomain = getFailureDomain(sc)
	}
	// Set default replication settings if necessary
	// Always set the default Size & ReplicasPerFailureDomain in arbiter mode
	defaultReplicatedSpec := generateCephReplicatedSpec(sc, poolTypeData)
	if poolSpec.Replicated.Size == 0 || arbiterEnabled(sc) {
		poolSpec.Replicated.Size = defaultReplicatedSpec.Size
	}
	if poolSpec.Replicated.ReplicasPerFailureDomain == 0 || arbiterEnabled(sc) {
		poolSpec.Replicated.ReplicasPerFailureDomain = defaultReplicatedSpec.ReplicasPerFailureDomain
	}
	if poolSpec.Replicated.TargetSizeRatio == 0.0 {
		poolSpec.Replicated.TargetSizeRatio = defaultReplicatedSpec.TargetSizeRatio
	}
}
