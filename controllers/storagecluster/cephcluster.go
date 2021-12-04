package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	v1 "github.com/openshift/api/config/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/defaults"
	statusutil "github.com/red-hat-storage/ocs-operator/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/tools/reference"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

type ocsCephCluster struct{}
type diskSpeed string

const (
	diskSpeedUnknown diskSpeed = "unknown"
	diskSpeedSlow    diskSpeed = "slow"
	diskSpeedFast    diskSpeed = "fast"
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
	{diskSpeedSlow, EBS, "gp2"},
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
	systemNodeCritical    = "system-node-critical"
	openshiftUserCritical = "openshift-user-critical"
)

func arbiterEnabled(sc *ocsv1.StorageCluster) bool {
	return sc.Spec.Arbiter.Enable
}

// ensureCreated ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (obj *ocsCephCluster) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	reconcileStrategy := ReconcileStrategy(sc.Spec.ManagedResources.CephCluster.ReconcileStrategy)
	if reconcileStrategy == ReconcileStrategyIgnore {
		return nil
	}

	if sc.Spec.ExternalStorage.Enable && len(sc.Spec.StorageDeviceSets) != 0 {
		return fmt.Errorf("'StorageDeviceSets' should not be initialized in an external CephCluster")
	}

	for i, ds := range sc.Spec.StorageDeviceSets {
		sc.Spec.StorageDeviceSets[i].Config.TuneSlowDeviceClass = false
		sc.Spec.StorageDeviceSets[i].Config.TuneFastDeviceClass = false

		diskSpeed, err := r.checkTuneStorageDevices(ds)
		if err != nil {
			return fmt.Errorf("Failed to check for known device types: %+v", err)
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
			return err
		}
	}

	var cephCluster *rookCephv1.CephCluster
	// Define a new CephCluster object
	if sc.Spec.ExternalStorage.Enable {
		extRArr, err := r.retrieveExternalSecretData(sc)
		if err != nil {
			r.Log.Error(err, "Could not retrieve the External Cluster details.",
				"CephCluster", klog.KRef(sc.Namespace, sc.Name))
			return err
		}
		endpointR, err := findNamedResourceFromArray(extRArr, "monitoring-endpoint")
		if err != nil {
			return err
		}
		monitoringIP := endpointR.Data["MonitoringEndpoint"]
		monitoringPort := endpointR.Data["MonitoringPort"]
		if err := verifyMonitoringEndpoints(monitoringIP, monitoringPort, r.Log); err != nil {
			r.Log.Error(err, "Could not connect to the Monitoring Endpoints.",
				"CephCluster", klog.KRef(sc.Namespace, sc.Name))
			return err
		}
		r.Log.Info("Monitoring Information found. Monitoring will be enabled on the external cluster.", "CephCluster", klog.KRef(sc.Namespace, sc.Name))
		cephCluster = newExternalCephCluster(sc, r.images.Ceph, monitoringIP, monitoringPort)
	} else {
		kmsConfigMap, err := getKMSConfigMap(KMSConfigMapName, sc, r.Client)
		if err != nil {
			r.Log.Error(err, "Failed to procure KMS ConfigMap.", "KMSConfigMap", klog.KRef(sc.Namespace, KMSConfigMapName))
			return err
		}
		if kmsConfigMap != nil {
			if err = reachKMSProvider(kmsConfigMap); err != nil {
				r.Log.Error(err, "Address provided in KMS ConfigMap is not reachable.", "KMSConfigMap", klog.KRef(kmsConfigMap.Namespace, kmsConfigMap.Name))
				return err
			}
		}
		cephCluster = newCephCluster(sc, r.images.Ceph, r.nodeCount, r.serverVersion, kmsConfigMap, r.Log)
	}

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.Scheme); err != nil {
		r.Log.Error(err, "Unable to set controller reference for CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
		return err
	}

	platform, err := r.platform.GetPlatform(r.Client)
	if err != nil {
		r.Log.Error(err, "Failed to get Platform.", "Platform", platform)
	} else if platform == v1.IBMCloudPlatformType || platform == IBMCloudCosPlatformType {
		r.Log.Info("Increasing Mon failover timeout to 15m.", "Platform", platform)
		cephCluster.Spec.HealthCheck.DaemonHealth.Monitor.Timeout = "15m"
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
				return err
			}
			// Need to happen after the ceph cluster CR creation was confirmed
			sc.Status.Images.Ceph.ActualImage = cephCluster.Spec.CephVersion.Image
			// Assuming progress when ceph cluster CR is created.
			reason := "CephClusterStatus"
			message := "CephCluster resource is not reporting status"
			statusutil.MapCephClusterNoConditions(&r.conditions, reason, message)
			return nil
		}
		r.Log.Error(err, "Unable to fetch CephCluster.", "CephCluster", klog.KRef(cephCluster.Namespace, cephCluster.Name))
		return err
	} else if reconcileStrategy == ReconcileStrategyInit {
		return nil
	}

	// Record actual Ceph container image version before attempting update
	sc.Status.Images.Ceph.ActualImage = found.Spec.CephVersion.Image

	// Add it to the list of RelatedObjects if found
	objectRef, err := reference.GetReference(r.Scheme, found)
	if err != nil {
		r.Log.Error(err, "Unable to get CephCluster ObjectReference.", "CephCluster", klog.KRef(found.Namespace, found.Name))
		return err
	}
	err = objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	if err != nil {
		r.Log.Error(err, "Unable to add CephCluster to the list of Related Objects in StorageCluster.", "CephCluster", klog.KRef(objectRef.Namespace, objectRef.Name), "StorageCluster", klog.KRef(sc.Namespace, sc.Name))
		return err
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
			return err
		}
	}

	return nil
}

// ensureDeleted deletes the CephCluster owned by the StorageCluster
func (obj *ocsCephCluster) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	cephCluster := &rookCephv1.CephCluster{}
	cephClusterName := generateNameForCephCluster(sc)
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephClusterName, Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster not found.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return nil
		}
		r.Log.Error(err, "Uninstall: Unable to retrieve CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
		return fmt.Errorf("Uninstall: Unable to retrieve cephCluster: %v", err)
	}

	if cephCluster.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
		err = r.Client.Delete(context.TODO(), cephCluster)
		if err != nil {
			r.Log.Error(err, "Uninstall: Failed to delete CephCluster.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return fmt.Errorf("uninstall: Failed to delete CephCluster: %v", err)
		}
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster is deleted.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
			return nil
		}
	}
	r.Log.Error(err, "Uninstall: Waiting for CephCluster to be deleted.", "CephCluster", klog.KRef(sc.Namespace, cephClusterName))
	return fmt.Errorf("uninstall: Waiting for CephCluster to be deleted")

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
func newCephCluster(sc *ocsv1.StorageCluster, cephImage string, nodeCount int, serverVersion *version.Info, kmsConfigMap *corev1.ConfigMap, reqLogger logr.Logger) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
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
			Mon:             generateMonSpec(sc, nodeCount),
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
				Enabled:        true,
				RulesNamespace: "openshift-storage",
			},
			Storage: rookCephv1.StorageScopeSpec{
				StorageClassDeviceSets: newStorageClassDeviceSets(sc, serverVersion),
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
			Resources: newCephDaemonResources(sc),
			ContinueUpgradeAfterChecksEvenIfNotHealthy: true,
			LogCollector: rookCephv1.LogCollectorSpec{
				Enabled:     true,
				Periodicity: "24h",
			},
			Labels: rookCephv1.LabelsSpec{
				rookCephv1.KeyMonitoring: getCephClusterMonitoringLabels(*sc),
			},
		},
	}

	monPVCTemplate := sc.Spec.MonPVCTemplate
	monDataDirHostPath := sc.Spec.MonDataDirHostPath
	// If the `monPVCTemplate` is provided, the mons will provisioned on the
	// provided `monPVCTemplate`.
	if monPVCTemplate != nil {
		cephCluster.Spec.Mon.VolumeClaimTemplate = monPVCTemplate
		// If the `monDataDirHostPath` is provided without the `monPVCTemplate`,
		// the mons will be provisioned on the provided `monDataDirHostPath`.
	} else if len(monDataDirHostPath) > 0 {
		cephCluster.Spec.DataDirHostPath = monDataDirHostPath
		// If no `monPVCTemplate` and `monDataDirHostPath` is provided, the mons will
		// be provisioned using the PVC template of first StorageDeviceSets if present.
	} else if len(sc.Spec.StorageDeviceSets) > 0 {
		ds := sc.Spec.StorageDeviceSets[0]
		cephCluster.Spec.Mon.VolumeClaimTemplate = &corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: ds.DataPVCTemplate.Spec.StorageClassName,
				Resources: corev1.ResourceRequirements{
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
		// Set default KMS_PROVIDER and VAULT_SECRET_ENGINE values, refer https://issues.redhat.com/browse/RHSTOR-1963
		if _, ok := kmsConfigMap.Data["KMS_PROVIDER"]; !ok {
			kmsConfigMap.Data["KMS_PROVIDER"] = "vault"
		}
		if _, ok := kmsConfigMap.Data["VAULT_SECRET_ENGINE"]; !ok {
			kmsConfigMap.Data["VAULT_SECRET_ENGINE"] = "kv"
		}
		cephCluster.Spec.Security.KeyManagementService.ConnectionDetails = kmsConfigMap.Data
		cephCluster.Spec.Security.KeyManagementService.TokenSecretName = KMSTokenSecretName
	}
	return cephCluster
}

func isMultus(nwSpec *rookCephv1.NetworkSpec) bool {
	if nwSpec != nil {
		return nwSpec.IsMultus()
	}
	return false
}

func validateMultusSelectors(selectors map[string]string) error {
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

// getNetworkSpec returns cephv1.NetworkSpec after reconciling the
// storageCluster.Spec.HostNetwork and storageCluster.Spec.Network fields
func getNetworkSpec(sc ocsv1.StorageCluster) rookCephv1.NetworkSpec {
	networkSpec := rookCephv1.NetworkSpec{}
	if sc.Spec.Network != nil {
		networkSpec = *sc.Spec.Network
	}
	// respect both the old way and the new way for enabling HostNetwork
	networkSpec.HostNetwork = networkSpec.HostNetwork || sc.Spec.HostNetwork
	return networkSpec
}

func newExternalCephCluster(sc *ocsv1.StorageCluster, cephImage, monitoringIP, monitoringPort string) *rookCephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	var monitoringSpec = rookCephv1.MonitoringSpec{Enabled: false}

	if monitoringIP != "" {
		monitoringSpec.Enabled = true
		monitoringSpec.RulesNamespace = sc.Namespace
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
			Labels: rookCephv1.LabelsSpec{
				rookCephv1.KeyMonitoring: getCephClusterMonitoringLabels(*sc),
			},
		},
	}

	return externalCephCluster
}

func getMinDeviceSetReplica(sc *ocsv1.StorageCluster) int {
	if arbiterEnabled(sc) {
		return defaults.ArbiterModeDeviceSetReplica
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

func getMonCount(nodeCount int, arbiter bool) int {
	// return static value if overridden
	override := os.Getenv(monCountOverrideEnvVar)
	if override != "" {
		count, err := strconv.Atoi(override)
		if err != nil {
			log.Error(err, "Could not decode env var.", "monCountOverrideEnvVar", monCountOverrideEnvVar)
		} else {
			return count
		}
	}

	if arbiter {
		return defaults.ArbiterModeMonCount
	}
	return defaults.DefaultMonCount
}

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(sc *ocsv1.StorageCluster, serverVersion *version.Info) []rookCephv1.StorageClassDeviceSet {
	storageDeviceSets := sc.Spec.StorageDeviceSets
	topologyMap := sc.Status.NodeTopologies

	var storageClassDeviceSets []rookCephv1.StorageClassDeviceSet

	// For kube server version 1.19 and above, topology spread constraints are used for OSD placements.
	// For kube server version below 1.19, NodeAffinity and PodAntiAffinity are used for OSD placements.
	supportTSC := serverVersion.Major >= defaults.KubeMajorTopologySpreadConstraints && serverVersion.Minor >= defaults.KubeMinorTopologySpreadConstraints

	for _, ds := range storageDeviceSets {
		resources := ds.Resources
		if resources.Requests == nil && resources.Limits == nil {
			resources = defaults.DaemonResources["osd"]
		}

		portable := ds.Portable

		topologyKey := ds.TopologyKey
		topologyKeyValues := []string{}

		noPlacement := ds.Placement.NodeAffinity == nil && ds.Placement.PodAffinity == nil && ds.Placement.PodAntiAffinity == nil
		noPreparePlacement := ds.PreparePlacement.NodeAffinity == nil && ds.PreparePlacement.PodAffinity == nil && ds.PreparePlacement.PodAntiAffinity == nil

		if supportTSC {
			noPlacement = noPlacement && ds.Placement.TopologySpreadConstraints == nil
			noPreparePlacement = noPreparePlacement && ds.PreparePlacement.TopologySpreadConstraints == nil
		}

		if noPlacement {
			if topologyKey == "" {
				topologyKey = getFailureDomain(sc)
			}

			if topologyKey == "host" {
				portable = false
			}

			if topologyMap != nil {
				topologyKey, topologyKeyValues = topologyMap.GetKeyValues(topologyKey)
			}
		}

		count, replica := countAndReplicaOf(&ds)
		for i := 0; i < replica; i++ {
			placement := rookCephv1.Placement{}
			preparePlacement := rookCephv1.Placement{}

			if noPlacement {
				if supportTSC {
					in := getPlacement(sc, "osd-tsc")
					(&in).DeepCopyInto(&placement)

					if noPreparePlacement {
						in := getPlacement(sc, "osd-prepare-tsc")
						(&in).DeepCopyInto(&preparePlacement)
					}

					if len(topologyKeyValues) >= getMinDeviceSetReplica(sc) {
						// Hard constraints are set in OSD placement for portable volumes with rack failure domain
						// domain as there is no node affinity in PVs. This restricts the movement of OSDs
						// between failure domain.
						if portable && !strings.Contains(topologyKey, "zone") {
							addStrictFailureDomainTSC(&placement, topologyKey)
						}
						// If topologyKey is not host, append additional topology spread constarint to the
						// default preparePlacement. This serves even distribution at the host level
						// within a failure domain (zone/rack).
						if noPreparePlacement {
							if topologyKey != corev1.LabelHostname {
								addStrictFailureDomainTSC(&preparePlacement, topologyKey)
							} else {
								preparePlacement.TopologySpreadConstraints[0].TopologyKey = topologyKey
							}
						}
					}
				} else {
					in := getPlacement(sc, "osd")
					(&in).DeepCopyInto(&placement)

					if noPreparePlacement {
						in := getPlacement(sc, "osd-prepare")
						(&in).DeepCopyInto(&preparePlacement)
					}

					if len(topologyKeyValues) >= getMinDeviceSetReplica(sc) {
						topologyIndex := i % len(topologyKeyValues)
						setTopologyForAffinity(&placement, topologyKeyValues[topologyIndex], topologyKey)
						if noPreparePlacement {
							setTopologyForAffinity(&preparePlacement, topologyKeyValues[topologyIndex], topologyKey)
						}
					}
				}

				if !noPreparePlacement {
					preparePlacement = ds.PreparePlacement
				}
			} else if !noPlacement && noPreparePlacement {
				preparePlacement = ds.Placement
				placement = ds.Placement
			} else {
				preparePlacement = ds.PreparePlacement
				placement = ds.Placement
			}

			// Annotation crushDeviceClass ensures osd with different CRUSH device class than the one detected by Ceph
			crushDeviceClass := ds.DeviceType
			if ds.DeviceClass != "" {
				crushDeviceClass = ds.DeviceClass
			}
			annotations := map[string]string{
				"crushDeviceClass": crushDeviceClass,
			}
			// Annotation crushInitialWeight is an optinal, explicit weight to set upon OSD's init (as float, in TiB units).
			// ROOK & Ceph do not want any (optional) Ti[B] suffix, so trim it here.
			// If not set, Ceph will define OSD's weight based on its capacity.
			crushInitialWeight := strings.TrimSuffix(strings.TrimSuffix(ds.InitialWeight, "Ti"), "TiB")
			if crushInitialWeight != "" {
				annotations["crushInitialWeight"] = crushInitialWeight
			}
			// Annotation crushPrimaryAffinity is an optinal, explicit primary-affinity value within the range [0,1) to
			// set upon OSD's deployment. If not set, Ceph sets default value to 1
			if ds.PrimaryAffinity != "" {
				annotations["crushPrimaryAffinity"] = ds.PrimaryAffinity
			}
			ds.DataPVCTemplate.Annotations = annotations

			set := rookCephv1.StorageClassDeviceSet{
				Name:                 fmt.Sprintf("%s-%d", ds.Name, i),
				Count:                count,
				Resources:            resources,
				Placement:            placement,
				PreparePlacement:     &preparePlacement,
				Config:               ds.Config.ToMap(),
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{ds.DataPVCTemplate},
				Portable:             portable,
				TuneSlowDeviceClass:  ds.Config.TuneSlowDeviceClass,
				TuneFastDeviceClass:  ds.Config.TuneFastDeviceClass,
				Encrypted:            sc.Spec.Encryption.Enable,
			}

			if ds.MetadataPVCTemplate != nil {
				ds.MetadataPVCTemplate.ObjectMeta.Name = metadataPVCName
				set.VolumeClaimTemplates = append(set.VolumeClaimTemplates, *ds.MetadataPVCTemplate)
			}
			if ds.WalPVCTemplate != nil {
				ds.WalPVCTemplate.ObjectMeta.Name = walPVCName
				set.VolumeClaimTemplates = append(set.VolumeClaimTemplates, *ds.WalPVCTemplate)
			}

			storageClassDeviceSets = append(storageClassDeviceSets, set)
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

func newCephDaemonResources(sc *ocsv1.StorageCluster) map[string]corev1.ResourceRequirements {

	custom := sc.Spec.Resources
	resources := map[string]corev1.ResourceRequirements{
		"mon": defaults.DaemonResources["mon"],
		"mgr": defaults.DaemonResources["mgr"],
		"mds": defaults.DaemonResources["mds"],
		"rgw": defaults.DaemonResources["rgw"],
	}
	if arbiterEnabled(sc) {
		resources["mgr-sidecar"] = defaults.DaemonResources["mgr-sidecar"]
	}

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

	tuneFastDevices, err := r.DevicesDefaultToFastForThisPlatform()
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
		stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, rookCephv1.StretchClusterZoneSpec{
			Name:    zone,
			Arbiter: false,
		})
	}

	arbiterZoneSpec := rookCephv1.StretchClusterZoneSpec{
		Name:    sc.Spec.NodeTopologies.ArbiterLocation,
		Arbiter: true,
	}
	if sc.Spec.Arbiter.ArbiterMonPVCTemplate != nil {
		arbiterZoneSpec.VolumeClaimTemplate = sc.Spec.Arbiter.ArbiterMonPVCTemplate
	}
	stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, arbiterZoneSpec)

	return &stretchClusterSpec
}

func generateMonSpec(sc *ocsv1.StorageCluster, nodeCount int) rookCephv1.MonSpec {
	if arbiterEnabled(sc) {
		return rookCephv1.MonSpec{
			Count:                getMonCount(nodeCount, true),
			AllowMultiplePerNode: false,
			StretchCluster:       generateStretchClusterSpec(sc),
		}
	}

	return rookCephv1.MonSpec{
		Count:                getMonCount(nodeCount, false),
		AllowMultiplePerNode: false,
	}
}

func generateMgrSpec(sc *ocsv1.StorageCluster) rookCephv1.MgrSpec {
	spec := rookCephv1.MgrSpec{
		Modules: []rookCephv1.Module{
			{Name: "pg_autoscaler", Enabled: true},
			{Name: "balancer", Enabled: true},
		},
	}

	if arbiterEnabled(sc) {
		spec.Count = 2
	}

	return spec
}

func getCephObjectStoreGatewayInstances(sc *ocsv1.StorageCluster) int32 {
	if arbiterEnabled(sc) {
		return int32(defaults.ArbiterCephObjectStoreGatewayInstances)
	}
	return int32(defaults.CephObjectStoreGatewayInstances)
}

// addStrictFailureDomainTSC adds hard topology constraints at failure domain level
// and uses soft topology constraints within falure domain (across host).
func addStrictFailureDomainTSC(placement *rookCephv1.Placement, topologyKey string) {
	newTSC := placement.TopologySpreadConstraints[0]
	newTSC.TopologyKey = topologyKey
	newTSC.WhenUnsatisfiable = "DoNotSchedule"

	placement.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{newTSC, placement.TopologySpreadConstraints[0]}
}
