package storagecluster

import (
	"context"
	"fmt"
	"os"
	"reflect"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"github.com/operator-framework/operator-sdk/pkg/ready"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1alpha2"
)

var monCount = defaults.MonCount

var validTopologyLabelKeys = []string{
	"failure-domain.beta.kubernetes.io",
	"failure-domain.kubernetes.io",
}

func init() {
	monCountStr := os.Getenv("MON_COUNT_OVERRIDE")
	if monCountStr == "" {
		return
	}

	count, err := strconv.Atoi(monCountStr)
	if err != nil {
		panic(err)
	}

	if count > 0 {
		monCount = count
		log.Info("Using MON_COUNT_OVERRIDE value %d", monCount)
	}
}

// Reconcile reads that state of the cluster for a StorageCluster object and makes changes based on the state read
// and what is in the StorageCluster.Spec
// Note:
// The Controller will requeue the Request to be processed again if the returned error is non-nil or
// Result.Requeue is true, otherwise upon completion it will remove the work from the queue.
func (r *ReconcileStorageCluster) Reconcile(request reconcile.Request) (reconcile.Result, error) {
	reqLogger := r.reqLogger.WithValues("Request.Namespace", request.Namespace, "Request.Name", request.Name)
	reqLogger.Info("Reconciling StorageCluster")

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
				reqLogger.Error(phaseErr, "Failed to set PhaseIgnored")
				return reconcile.Result{}, phaseErr
			}
			return reconcile.Result{}, nil
		}
	} else if instance.Status.Phase == statusutil.PhaseIgnored {
		return reconcile.Result{}, nil
	}

	if instance.Status.Phase != statusutil.PhaseReady &&
		instance.Status.Phase != statusutil.PhaseClusterExpanding {
		instance.Status.Phase = statusutil.PhaseProgressing
		phaseErr := r.client.Status().Update(context.TODO(), instance)
		if phaseErr != nil {
			reqLogger.Error(phaseErr, "Failed to set PhaseProgressing")
		}
	}

	// Add conditions if there are none
	if instance.Status.Conditions == nil {
		reason := ocsv1.ReconcileInit
		message := "Initializing StorageCluster"
		statusutil.SetProgressingCondition(&instance.Status.Conditions, reason, message)
		err = r.client.Status().Update(context.TODO(), instance)
		if err != nil {
			reqLogger.Error(err, "Failed to add conditions to status")
			return reconcile.Result{}, err
		}
	}

	// Get storage node topology labels
	err = r.reconcileNodeTopologyMap(instance, reqLogger)
	if err != nil {
		reqLogger.Error(err, "Failed to set node topology map")
		return reconcile.Result{}, err
	}

	// Check for StorageClusterInitialization
	scinit := &ocsv1.StorageClusterInitialization{}
	err = r.client.Get(context.TODO(), request.NamespacedName, scinit)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating StorageClusterInitialization resource")

			// if the StorageClusterInitialization object doesn't exist
			// ensure we re-reconcile on all initialization resources
			instance.Status.StorageClassesCreated = false
			instance.Status.CephObjectStoresCreated = false
			instance.Status.CephBlockPoolsCreated = false
			instance.Status.CephObjectStoreUsersCreated = false
			instance.Status.CephFilesystemsCreated = false
			instance.Status.FailureDomain = determineFailureDomain(instance.Status.NodeTopologies)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				return reconcile.Result{}, err
			}

			scinit.Name = request.Name
			scinit.Namespace = request.Namespace
			if err = controllerutil.SetControllerReference(instance, scinit, r.scheme); err != nil {
				return reconcile.Result{}, err
			}

			err = r.client.Create(context.TODO(), scinit)
			switch {
			case err == nil:
				log.Info("Created StorageClusterInitialization resource")
			case errors.IsAlreadyExists(err):
				log.Info("StorageClusterInitialization resource already exists")
			default:
				log.Error(err, "Failed to create StorageClusterInitialization resource")
				return reconcile.Result{}, err
			}
		} else {
			// Error reading the object - requeue the request.
			return reconcile.Result{}, err
		}
	}

	// in-memory conditions should start off empty. It will only ever hold
	// negative conditions (!Available, Degraded, Progressing)
	r.conditions = nil
	// Start with empty r.phase
	r.phase = ""

	for _, f := range []func(*ocsv1.StorageCluster, logr.Logger) error{
		// Add support for additional resources here
		r.ensureStorageClasses,
		r.ensureCephObjectStores,
		r.ensureCephObjectStoreUsers,
		r.ensureCephBlockPools,
		r.ensureCephFilesystems,

		r.ensureCephCluster,
		r.ensureNoobaaSystem,
	} {
		err = f(instance, reqLogger)
		if r.phase == statusutil.PhaseClusterExpanding {
			instance.Status.Phase = statusutil.PhaseClusterExpanding
			phaseErr := r.client.Status().Update(context.TODO(), instance)
			if phaseErr != nil {
				reqLogger.Error(phaseErr, "Failed to set PhaseClusterExpanding")
			}
		} else {
			if instance.Status.Phase != statusutil.PhaseReady {
				instance.Status.Phase = statusutil.PhaseProgressing
				phaseErr := r.client.Status().Update(context.TODO(), instance)
				if phaseErr != nil {
					reqLogger.Error(phaseErr, "Failed to set PhaseProgressing")
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
				reqLogger.Error(uErr, "Failed to update status")
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
		r := ready.NewFileReady()
		err = r.Set()
		if err != nil {
			reqLogger.Error(err, "Failed to mark operator ready")
			return reconcile.Result{}, err
		}
		if instance.Status.Phase != statusutil.PhaseClusterExpanding {
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
			r := ready.NewFileReady()
			err = r.Unset()
			if err != nil {
				reqLogger.Error(err, "Failed to mark operator unready")
				return reconcile.Result{}, err
			}
		}
		if instance.Status.Phase != statusutil.PhaseClusterExpanding {
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
		reqLogger.Error(phaseErr, "Failed to update status")
	}
	return reconcile.Result{}, phaseErr
}

// reconcileNodeTopologyMap builds the map of all topology labels on all nodes
// in the storage cluster
func (r *ReconcileStorageCluster) reconcileNodeTopologyMap(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	nodes := &corev1.NodeList{}

	nodeMatchLabel := map[string]string{defaults.NodeAffinityKey: ""}
	err := r.client.List(context.TODO(), nodes, client.MatchingLabels(nodeMatchLabel))
	if err != nil {
		return err
	}

	if sc.Status.NodeTopologies == nil {
		sc.Status.NodeTopologies = ocsv1.NewNodeTopologyMap()
	}
	topologyMap := sc.Status.NodeTopologies
	updated := false

	for _, node := range nodes.Items {
		labels := node.Labels
		for label, value := range labels {
			for _, key := range validTopologyLabelKeys {
				if strings.Contains(label, key) {
					if !topologyMap.Contains(label, value) {
						reqLogger.Info("Adding topology label from node", "Node", node.Name, "Label", label, "Value", value)
						topologyMap.Add(label, value)
						updated = true
					}
				}
			}
		}

	}

	if updated {
		reqLogger.Info("Updating node topology map for StorageCluster")
		err = r.client.Status().Update(context.TODO(), sc)
		if err != nil {
			return err
		}
	}

	return nil
}

// ensureCephCluster ensures that a CephCluster resource exists with its Spec in
// the desired state.
func (r *ReconcileStorageCluster) ensureCephCluster(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	// Define a new CephCluster object
	cephCluster := newCephCluster(sc, r.cephImage)

	// Set StorageCluster instance as the owner and controller
	if err := controllerutil.SetControllerReference(sc, cephCluster, r.scheme); err != nil {
		return err
	}

	// Check if this CephCluster already exists
	found := &cephv1.CephCluster{}
	err := r.client.Get(context.TODO(), types.NamespacedName{Name: cephCluster.Name, Namespace: cephCluster.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating CephCluster")
			return r.client.Create(context.TODO(), cephCluster)
		}
		return err
	}

	// Update the CephCluster if it is not in the desired state
	if !reflect.DeepEqual(cephCluster.Spec, found.Spec) {
		reqLogger.Info("Updating spec for CephCluster")
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
		statusutil.MapCephClusterNegativeConditions(&r.conditions, found)
	}

	// When phase is expanding, wait for CephCluster state to be updating
	// this means expansion is in progress and overall system is progressing
	// else expansion is not yet triggered
	if sc.Status.Phase == statusutil.PhaseClusterExpanding &&
		found.Status.State != cephv1.ClusterStateUpdating {
		r.phase = statusutil.PhaseClusterExpanding
	}
	return nil
}

// determineFailureDomain determines the appropriate Ceph failure domain based
// on the storage cluster's topology map
func determineFailureDomain(topologyMap *ocsv1.NodeTopologyMap) string {
	failureDomain := "host"

	for label, labelValues := range topologyMap.Labels {
		if strings.Contains(label, "zone") {
			if len(labelValues) >= 3 {
				failureDomain = "zone"
			}
		}
	}

	return failureDomain
}

// newCephCluster returns a CephCluster object.
func newCephCluster(sc *ocsv1.StorageCluster, cephImage string) *cephv1.CephCluster {
	labels := map[string]string{
		"app": sc.Name,
	}

	cephCluster := &cephv1.CephCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      sc.Name,
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: cephv1.ClusterSpec{
			CephVersion: cephv1.CephVersionSpec{
				Image:            cephImage,
				AllowUnsupported: false,
			},
			Mon: cephv1.MonSpec{
				Count:                monCount,
				AllowMultiplePerNode: false,
			},
			Mgr: cephv1.MgrSpec{
				Modules: []cephv1.Module{
					cephv1.Module{Name: "pg_autoscaler", Enabled: true},
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
				StorageClassDeviceSets: newStorageClassDeviceSets(sc.Spec.StorageDeviceSets, sc.Status.NodeTopologies),
				TopologyAware:          true,
			},
			Placement: rook.PlacementSpec{
				"all": defaults.DaemonPlacements["all"],
			},
			Resources: newCephDaemonResources(sc.Spec.Resources),
		},
	}

	// If a MonPVCTemplate is provided, use that. If not, if StorageDeviceSets
	// have been provided, use the StorageClass of the DataPVCTemplate from the
	// first StorageDeviceSet for providing the Mon PVs
	if sc.Spec.MonPVCTemplate != nil {
		cephCluster.Spec.Mon.VolumeClaimTemplate = sc.Spec.MonPVCTemplate
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
	}

	return cephCluster
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

// newStorageClassDeviceSets converts a list of StorageDeviceSets into a list of Rook StorageClassDeviceSets
func newStorageClassDeviceSets(storageDeviceSets []ocsv1.StorageDeviceSet, topologyMap *ocsv1.NodeTopologyMap) []rook.StorageClassDeviceSet {
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
				topologyKey = "zone"
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
			portable := ds.Portable

			if noPlacement {
				in := defaults.DaemonPlacements["osd"]
				(&in).DeepCopyInto(&placement)

				if len(topologyKeyValues) >= replica {
					portable = true
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
				} else {
					portable = false
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
				Portable:             portable,
			}
			storageClassDeviceSets = append(storageClassDeviceSets, set)
		}
	}

	return storageClassDeviceSets
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
