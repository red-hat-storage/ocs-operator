package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	statusutil "github.com/openshift/ocs-operator/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
	"k8s.io/client-go/tools/reference"
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
	// Hardcoding networkProvider to multus and this can be changed later to accomodate other providers
	networkProvider           = "multus"
	publicNetworkSelectorKey  = "public"
	clusterNetworkSelectorKey = "cluster"
)

func arbiterEnabled(sc *ocsv1.StorageCluster) bool {
	return sc.Spec.Arbiter.Enable
}

// ensureCreated ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (obj *ocsCephCluster) ensureCreated(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
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
			return err
		}
	}

	var cephCluster *cephv1.CephCluster
	// Define a new CephCluster object
	if sc.Spec.ExternalStorage.Enable {
		cephCluster = newExternalCephCluster(sc, r.images.Ceph, r.monitoringIP)
	} else {
		kmsConfigMap, err := getKMSConfigMap(sc, r.Client, reachKMSProvider)
		if err != nil {
			r.Log.Error(err, "failed to procure KMS config")
			return err
		}
		cephCluster = newCephCluster(sc, r.images.Ceph, r.nodeCount, r.serverVersion, kmsConfigMap, r.Log)
	}

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.Scheme); err != nil {
		return err
	}

	// Check if this CephCluster already exists
	found := &cephv1.CephCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			if sc.Spec.ExternalStorage.Enable {
				r.Log.Info("Creating external CephCluster")
			} else {
				r.Log.Info("Creating CephCluster")
			}
			if err := r.Client.Create(context.TODO(), cephCluster); err != nil {
				return err
			}
			// Need to happen after the ceph cluster CR creation was confirmed
			sc.Status.Images.Ceph.ActualImage = cephCluster.Spec.CephVersion.Image
			return nil
		}
		return err
	}

	// Update the CephCluster if it is not in the desired state
	if !reflect.DeepEqual(cephCluster.Spec, found.Spec) {
		r.Log.Info("Updating spec for CephCluster")
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
			return err
		}
		// Need to happen after the ceph cluster CR update was confirmed
		sc.Status.Images.Ceph.ActualImage = cephCluster.Spec.CephVersion.Image
		return nil
	}

	// Add it to the list of RelatedObjects if found
	objectRef, err := reference.GetReference(r.Scheme, found)
	if err != nil {
		return err
	}
	err = objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)
	if err != nil {
		return err
	}

	// Handle CephCluster resource status
	if found.Status.State == "" {
		r.Log.Info("CephCluster resource is not reporting status.")
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
		found.Status.State != cephv1.ClusterStateUpdating {
		r.phase = statusutil.PhaseClusterExpanding
	}

	if sc.Spec.ExternalStorage.Enable {
		if found.Status.State == cephv1.ClusterStateConnecting {
			sc.Status.Phase = statusutil.PhaseConnecting
		} else if found.Status.State == cephv1.ClusterStateConnected {
			sc.Status.Phase = statusutil.PhaseReady
		} else {
			sc.Status.Phase = statusutil.PhaseNotReady
		}
	}

	return nil
}

// ensureDeleted deletes the CephCluster owned by the StorageCluster
func (obj *ocsCephCluster) ensureDeleted(r *StorageClusterReconciler, sc *ocsv1.StorageCluster) error {
	cephCluster := &cephv1.CephCluster{}
	err := r.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster not found")
			return nil
		}
		return fmt.Errorf("Uninstall: Unable to retrieve cephCluster: %v", err)
	}

	if cephCluster.GetDeletionTimestamp().IsZero() {
		r.Log.Info("Uninstall: Deleting cephCluster")
		err = r.Client.Delete(context.TODO(), cephCluster)
		if err != nil {
			return fmt.Errorf("Uninstall: Failed to delete cephCluster: %v", err)
		}
	}

	err = r.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(sc), Namespace: sc.Namespace}, cephCluster)
	if err != nil {
		if errors.IsNotFound(err) {
			r.Log.Info("Uninstall: CephCluster is deleted")
			return nil
		}
	}
	return fmt.Errorf("Uninstall: Waiting for cephCluster to be deleted")

}

// newCephCluster returns a CephCluster object.
func newCephCluster(sc *ocsv1.StorageCluster, cephImage string, nodeCount int, serverVersion *version.Info, kmsConfigMap *corev1.ConfigMap, reqLogger logr.Logger) *cephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	cephCluster := &cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephCluster(sc),
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: cephv1.ClusterSpec{
			CephVersion: cephv1.CephVersionSpec{
				Image:            cephImage,
				AllowUnsupported: allowUnsupportedCephVersion(),
			},
			Mon: generateMonSpec(sc, nodeCount),
			Mgr: cephv1.MgrSpec{
				Modules: []cephv1.Module{
					{Name: "pg_autoscaler", Enabled: true},
					{Name: "balancer", Enabled: true},
				},
			},
			DataDirHostPath: "/var/lib/rook",
			DisruptionManagement: cephv1.DisruptionManagementSpec{
				ManagePodBudgets:                 true,
				ManageMachineDisruptionBudgets:   false,
				MachineDisruptionBudgetNamespace: "openshift-machine-api",
			},
			Network: cephv1.NetworkSpec{
				HostNetwork: sc.Spec.HostNetwork,
			},
			Monitoring: cephv1.MonitoringSpec{
				Enabled:        true,
				RulesNamespace: "openshift-storage",
			},
			Storage: rook.StorageScopeSpec{
				StorageClassDeviceSets: newStorageClassDeviceSets(sc, serverVersion),
			},
			Placement: rook.PlacementSpec{
				"all":     getPlacement(sc, "all"),
				"mon":     getPlacement(sc, "mon"),
				"arbiter": getPlacement(sc, "arbiter"),
			},
			Resources: newCephDaemonResources(sc.Spec.Resources),
			ContinueUpgradeAfterChecksEvenIfNotHealthy: true,
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
						corev1.ResourceStorage: resource.MustParse("10Gi"),
					},
				},
			},
		}
	} else {
		reqLogger.Info(fmt.Sprintf("No monDataDirHostPath, monPVCTemplate or storageDeviceSets configured for storageCluster %s", sc.GetName()))
	}
	if isMultus(sc.Spec.Network) {
		cephCluster.Spec.Network.NetworkSpec = *sc.Spec.Network
	}
	// if kmsConfig is not 'nil', add the KMS details to CephCluster spec
	if kmsConfigMap != nil {
		cephCluster.Spec.Security.KeyManagementService.ConnectionDetails = kmsConfigMap.Data
		cephCluster.Spec.Security.KeyManagementService.TokenSecretName = KMSTokenSecretName
	}
	return cephCluster
}

func isMultus(nwSpec *rook.NetworkSpec) bool {
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
		return fmt.Errorf("Both public and cluster network selector values can't be empty")
	}
	if publicNetwork == "" {
		return fmt.Errorf("public network selector values can't be empty")
	}
	return nil
}

func newExternalCephCluster(sc *ocsv1.StorageCluster, cephImage string, monitoringIP string) *cephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	var monitoringSpec = cephv1.MonitoringSpec{Enabled: false}

	if monitoringIP != "" {
		monitoringSpec = cephv1.MonitoringSpec{
			Enabled:              true,
			RulesNamespace:       sc.Namespace,
			ExternalMgrEndpoints: []corev1.EndpointAddress{{IP: monitoringIP}},
		}
	}

	externalCephCluster := &cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      generateNameForCephCluster(sc),
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: cephv1.ClusterSpec{
			External: cephv1.ExternalSpec{
				Enable: true,
			},
			CrashCollector: cephv1.CrashCollectorSpec{
				Disable: true,
			},
			DisruptionManagement: cephv1.DisruptionManagementSpec{
				ManagePodBudgets:               false,
				ManageMachineDisruptionBudgets: false,
			},
			Monitoring: monitoringSpec,
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

	return getMinDeviceSetReplica(sc) * getReplicasPerFailureDomain(sc)
}

func getMonCount(nodeCount int, arbiter bool) int {
	// return static value if overriden
	override := os.Getenv(monCountOverrideEnvVar)
	if override != "" {
		count, err := strconv.Atoi(override)
		if err != nil {
			log.Error(err, "could not decode env var %s", monCountOverrideEnvVar)
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
func newStorageClassDeviceSets(sc *ocsv1.StorageCluster, serverVersion *version.Info) []rook.StorageClassDeviceSet {
	storageDeviceSets := sc.Spec.StorageDeviceSets
	topologyMap := sc.Status.NodeTopologies

	var storageClassDeviceSets []rook.StorageClassDeviceSet

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
				topologyKey = determineFailureDomain(sc)
			}

			if topologyKey == "host" {
				portable = false
			}

			if topologyMap != nil {
				topologyKey, topologyKeyValues = topologyMap.GetKeyValues(topologyKey)
			}
		}

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

		for i := 0; i < replica; i++ {
			placement := rook.Placement{}
			preparePlacement := rook.Placement{}

			if noPlacement {
				if supportTSC {
					in := getPlacement(sc, "osd-tsc")
					(&in).DeepCopyInto(&placement)

					if noPreparePlacement {
						in := getPlacement(sc, "osd-prepare-tsc")
						(&in).DeepCopyInto(&preparePlacement)

						if len(topologyKeyValues) >= replica {
							// If topologyKey is not host, append additional topology spread constarint to the
							// default preparePlacement. This serves even distribution at the host level
							// within a failure domain (zone/rack).
							if topologyKey != corev1.LabelHostname {
								preparePlacement.TopologySpreadConstraints = append(preparePlacement.TopologySpreadConstraints, preparePlacement.TopologySpreadConstraints[0])
							}
							preparePlacement.TopologySpreadConstraints[0].TopologyKey = topologyKey
						}
					}
				} else {
					in := getPlacement(sc, "osd")
					(&in).DeepCopyInto(&placement)

					if noPreparePlacement {
						in := getPlacement(sc, "osd-prepare")
						(&in).DeepCopyInto(&preparePlacement)
					}

					if len(topologyKeyValues) >= replica {
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
			annotations := map[string]string{
				"crushDeviceClass": ds.DeviceType,
			}
			ds.DataPVCTemplate.Annotations = annotations

			set := rook.StorageClassDeviceSet{
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

func newCephDaemonResources(custom map[string]corev1.ResourceRequirements) map[string]corev1.ResourceRequirements {
	resources := map[string]corev1.ResourceRequirements{
		"mon": defaults.GetDaemonResources("mon", custom),
		"mgr": defaults.GetDaemonResources("mgr", custom),
	}

	for k := range resources {
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

func generateStretchClusterSpec(sc *ocsv1.StorageCluster) *cephv1.StretchClusterSpec {
	var zones []string
	stretchClusterSpec := cephv1.StretchClusterSpec{}
	stretchClusterSpec.FailureDomainLabel, zones = sc.Status.NodeTopologies.GetKeyValues(determineFailureDomain(sc))

	for _, zone := range zones {
		if zone == sc.Spec.NodeTopologies.ArbiterLocation {
			continue
		}
		stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, cephv1.StretchClusterZoneSpec{
			Name:    zone,
			Arbiter: false,
		})
	}

	arbiterZoneSpec := cephv1.StretchClusterZoneSpec{
		Name:    sc.Spec.NodeTopologies.ArbiterLocation,
		Arbiter: true,
	}
	if sc.Spec.Arbiter.ArbiterMonPVCTemplate != nil {
		arbiterZoneSpec.VolumeClaimTemplate = sc.Spec.Arbiter.ArbiterMonPVCTemplate
	}
	stretchClusterSpec.Zones = append(stretchClusterSpec.Zones, arbiterZoneSpec)

	return &stretchClusterSpec
}

func generateMonSpec(sc *ocsv1.StorageCluster, nodeCount int) cephv1.MonSpec {
	if arbiterEnabled(sc) {
		return cephv1.MonSpec{
			Count:                getMonCount(nodeCount, true),
			AllowMultiplePerNode: false,
			StretchCluster:       generateStretchClusterSpec(sc),
		}
	}

	return cephv1.MonSpec{
		Count:                getMonCount(nodeCount, false),
		AllowMultiplePerNode: false,
	}
}
