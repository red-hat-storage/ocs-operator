package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"

	"github.com/go-logr/logr"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

// ensureCephCluster ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (r *ReconcileStorageCluster) ensureCephCluster(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	if sc.Spec.ExternalStorage.Enable && len(sc.Spec.StorageDeviceSets) != 0 {
		return fmt.Errorf("'StorageDeviceSets' should not be initialized in an external CephCluster")
	}
	// if StorageClass is "gp2" or "io1" based, set tuneSlowDeviceClass to true
	// this is for performance optimization of slow device class
	//TODO: If for a StorageDeviceSet there is a separate metadata pvc template, check for StorageClass of data pvc template only
	for i, ds := range sc.Spec.StorageDeviceSets {
		throttle, err := r.throttleStorageDevices(*ds.DataPVCTemplate.Spec.StorageClassName)
		if err != nil {
			return fmt.Errorf("Failed to verify StorageClass provisioner. %+v", err)
		}
		if throttle {
			sc.Spec.StorageDeviceSets[i].Config.TuneSlowDeviceClass = true
		} else {
			sc.Spec.StorageDeviceSets[i].Config.TuneSlowDeviceClass = false
		}
	}

	var cephCluster *cephv1.CephCluster
	// Define a new CephCluster object
	if sc.Spec.ExternalStorage.Enable {
		cephCluster = newExternalCephCluster(sc, r.cephImage)
	} else {
		cephCluster = newCephCluster(sc, r.cephImage, r.nodeCount, reqLogger)
	}

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.scheme); err != nil {
		return err
	}

	// Check if this CephCluster already exists
	found := &cephv1.CephCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			if sc.Spec.ExternalStorage.Enable {
				reqLogger.Info("Creating external CephCluster")
			} else {
				reqLogger.Info("Creating CephCluster")
			}
			return r.client.Create(context.TODO(), cephCluster)
		}
		return err
	}

	// Update the CephCluster if it is not in the desired state
	if !reflect.DeepEqual(cephCluster.Spec, found.Spec) {
		reqLogger.Info("Updating spec for CephCluster")
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
		return r.client.Update(context.TODO(), found)
	}

	// Add it to the list of RelatedObjects if found
	objectRef, err := reference.GetReference(r.scheme, found)
	if err != nil {
		return err
	}
	objectreferencesv1.SetObjectReference(&sc.Status.RelatedObjects, *objectRef)

	// Handle CephCluster resource status
	if found.Status.State == "" {
		reqLogger.Info("CephCluster resource is not reporting status.")
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

		if err = r.client.Status().Update(context.TODO(), sc); err != nil {
			reqLogger.Error(err, "Failed to update external cluster status")
			return err
		}
	}

	return nil
}

// newCephCluster returns a CephCluster object.
func newCephCluster(sc *ocsv1.StorageCluster, cephImage string, nodeCount int, reqLogger logr.Logger) *cephv1.CephCluster {
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
				AllowUnsupported: false,
			},
			Mon: cephv1.MonSpec{
				Count:                getMonCount(nodeCount),
				AllowMultiplePerNode: false,
			},
			Mgr: cephv1.MgrSpec{
				Modules: []cephv1.Module{
					cephv1.Module{Name: "pg_autoscaler", Enabled: true},
					cephv1.Module{Name: "balancer", Enabled: true},
				},
			},
			DataDirHostPath: "/var/lib/rook",
			DisruptionManagement: cephv1.DisruptionManagementSpec{
				ManagePodBudgets:                 true,
				ManageMachineDisruptionBudgets:   false,
				MachineDisruptionBudgetNamespace: "openshift-machine-api",
			},
			RBDMirroring: cephv1.RBDMirroringSpec{
				Workers: 0,
			},
			Network: cephv1.NetworkSpec{
				HostNetwork: sc.Spec.HostNetwork,
			},
			Monitoring: cephv1.MonitoringSpec{
				Enabled:        true,
				RulesNamespace: "openshift-storage",
			},
			Storage: rook.StorageScopeSpec{
				StorageClassDeviceSets: newStorageClassDeviceSets(sc),
			},
			Placement: rook.PlacementSpec{
				"all": getPlacement(sc, "all"),
				"mon": getPlacement(sc, "mon"),
				"rgw": getPlacement(sc, "rgw"),
				"mds": getPlacement(sc, "mds"),
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
	return cephCluster
}

func newExternalCephCluster(sc *ocsv1.StorageCluster, cephImage string) *cephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
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
		},
	}
	return externalCephCluster
}

func getMonCount(nodeCount int) int {
	count := defaults.MonCountMin

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

	if nodeCount >= defaults.MonCountMax {
		count = defaults.MonCountMax
	}

	return count
}

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(sc *ocsv1.StorageCluster) []rook.StorageClassDeviceSet {
	storageDeviceSets := sc.Spec.StorageDeviceSets
	topologyMap := sc.Status.NodeTopologies

	var storageClassDeviceSets []rook.StorageClassDeviceSet

	for _, ds := range storageDeviceSets {
		resources := ds.Resources
		if resources.Requests == nil && resources.Limits == nil {
			resources = defaults.DaemonResources["osd"]
		}

		topologyKey := ds.TopologyKey
		topologyKeyValues := []string{}
		noPlacement := ds.Placement.NodeAffinity == nil && ds.Placement.PodAffinity == nil && ds.Placement.PodAntiAffinity == nil

		if noPlacement {
			if topologyKey == "" {
				topologyKey = determineFailureDomain(sc)
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

			if noPlacement {
				in := getPlacement(sc, "osd")
				(&in).DeepCopyInto(&placement)

				if len(topologyKeyValues) >= replica {
					podAffinityTerms := placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
					podAffinityTerms[0].PodAffinityTerm.TopologyKey = topologyKey

					topologyIndex := i % len(topologyKeyValues)
					nodeZoneSelector := corev1.NodeSelectorRequirement{
						Key:      topologyKey,
						Operator: corev1.NodeSelectorOpIn,
						Values:   []string{topologyKeyValues[topologyIndex]},
					}
					nodeSelectorTerms := placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms
					nodeSelectorTerms[0].MatchExpressions = append(nodeSelectorTerms[0].MatchExpressions, nodeZoneSelector)
				}
			} else {
				placement = ds.Placement
			}

			set := rook.StorageClassDeviceSet{
				Name:                 fmt.Sprintf("%s-%d", ds.Name, i),
				Count:                count,
				Resources:            resources,
				Placement:            placement,
				Config:               ds.Config.ToMap(),
				VolumeClaimTemplates: []corev1.PersistentVolumeClaim{ds.DataPVCTemplate},
				Portable:             ds.Portable,
				TuneSlowDeviceClass:  ds.Config.TuneSlowDeviceClass,
			}

			if ds.MetadataPVCTemplate != nil {
				ds.MetadataPVCTemplate.ObjectMeta.Name = metadataPVCName
				set.VolumeClaimTemplates = []corev1.PersistentVolumeClaim{ds.DataPVCTemplate, *ds.MetadataPVCTemplate}
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
