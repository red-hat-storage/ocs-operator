package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	objectreferencesv1 "github.com/openshift/custom-resource-status/objectreferences/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/openshift/ocs-operator/version"
	"github.com/operator-framework/operator-sdk/pkg/ready"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rook "github.com/rook/rook/pkg/apis/rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"k8s.io/client-go/tools/reference"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

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
)

var storageClusterFinalizer = "storagecluster.ocs.openshift.io"

var validTopologyLabelKeys = []string{
	"failure-domain.beta.kubernetes.io",
	"failure-domain.kubernetes.io",
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
				reqLogger.Error(phaseErr, "Failed to set PhaseIgnored")
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

	// Check GetDeletionTimestamp to determine if the object is under deletion
	if instance.GetDeletionTimestamp().IsZero() {
		if !contains(instance.GetFinalizers(), storageClusterFinalizer) {
			reqLogger.Info("Finalizer not found for storagecluster. Adding finalizer")
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Error(err, "Failed to update storagecluster with finalizer")
				return reconcile.Result{}, err
			}
		}
	} else {
		// The object is marked for deletion
		instance.Status.Phase = statusutil.PhaseDeleting
		phaseErr := r.client.Status().Update(context.TODO(), instance)
		if phaseErr != nil {
			reqLogger.Error(phaseErr, "Failed to set PhaseDeleting")
		}
		if contains(instance.GetFinalizers(), storageClusterFinalizer) {
			isDeleted, err := r.deleteResources(instance, reqLogger)
			if err != nil {
				// If the dependencies failed to delete because of errors, retry again
				return reconcile.Result{}, err
			}
			if isDeleted {
				reqLogger.Info("Removing finalizer")
				// Once all finalizers have been removed, the object will be deleted
				instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
				if err := r.client.Update(context.TODO(), instance); err != nil {
					reqLogger.Error(err, "Failed to remove finalizer from storagecluster")
					return reconcile.Result{}, err
				}
			} else {
				// Watch resources and events and reconcile.
				return reconcile.Result{}, nil
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
		if err := r.ensureStorageClusterInit(instance, request, reqLogger); err != nil {
			reqLogger.Error(err, "Failed to initialize the storagecluster")
			return reconcile.Result{}, err
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
			r.ensureCephCluster,
		}
	}
	for _, f := range ensureFs {
		err = f(instance, reqLogger)
		if r.phase == statusutil.PhaseClusterExpanding {
			instance.Status.Phase = statusutil.PhaseClusterExpanding
			phaseErr := r.client.Status().Update(context.TODO(), instance)
			if phaseErr != nil {
				reqLogger.Error(phaseErr, "Failed to set PhaseClusterExpanding")
			}
		} else {
			if instance.Status.Phase != statusutil.PhaseReady &&
				instance.Status.Phase != statusutil.PhaseConnecting {
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
			r := ready.NewFileReady()
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
		reqLogger.Error(phaseErr, "Failed to update status")
		return reconcile.Result{}, phaseErr
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

// ensureStorageClusterInit function initialize the StorageCluster
func (r *ReconcileStorageCluster) ensureStorageClusterInit(
	instance *ocsv1.StorageCluster,
	request reconcile.Request,
	reqLogger logr.Logger) error {
	// Check for StorageClusterInitialization
	scinit := &ocsv1.StorageClusterInitialization{}
	if err := r.client.Get(context.TODO(), request.NamespacedName, scinit); err != nil {
		if errors.IsNotFound(err) {
			reqLogger.Info("Creating StorageClusterInitialization resource")

			// if the StorageClusterInitialization object doesn't exist
			// ensure we re-reconcile on all initialization resources
			instance.Status.StorageClassesCreated = false
			instance.Status.CephObjectStoresCreated = false
			instance.Status.CephBlockPoolsCreated = false
			instance.Status.CephObjectStoreUsersCreated = false
			instance.Status.CephFilesystemsCreated = false
			instance.Status.FailureDomain = determineFailureDomain(instance)
			err = r.client.Status().Update(context.TODO(), instance)
			if err != nil {
				return err
			}

			scinit.Name = request.Name
			scinit.Namespace = request.Namespace
			if err = controllerutil.SetControllerReference(instance, scinit, r.scheme); err != nil {
				return err
			}

			err = r.client.Create(context.TODO(), scinit)
			switch {
			case err == nil:
				log.Info("Created StorageClusterInitialization resource")
			case errors.IsAlreadyExists(err):
				log.Info("StorageClusterInitialization resource already exists")
			default:
				log.Error(err, "Failed to create StorageClusterInitialization resource")
				return err
			}
		} else {
			// Error reading the object - requeue the request.
			return err
		}
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
	}

	return nil
}

// reconcileNodeTopologyMap builds the map of all topology labels on all nodes
// in the storage cluster
func (r *ReconcileStorageCluster) reconcileNodeTopologyMap(sc *ocsv1.StorageCluster, reqLogger logr.Logger) error {
	minNodes := defaults.DeviceSetReplica
	for _, deviceSet := range sc.Spec.StorageDeviceSets {
		if deviceSet.Replica > minNodes {
			minNodes = deviceSet.Replica
		}
	}

	nodes := &corev1.NodeList{}

	nodeMatchLabel := map[string]string{defaults.NodeAffinityKey: ""}
	err := r.client.List(context.TODO(), nodes, client.MatchingLabels(nodeMatchLabel))
	if err != nil {
		return err
	}

	if sc.Status.NodeTopologies == nil || sc.Status.NodeTopologies.Labels == nil {
		sc.Status.NodeTopologies = ocsv1.NewNodeTopologyMap()
	}
	topologyMap := sc.Status.NodeTopologies
	updated := false
	nodeRacks := ocsv1.NewNodeTopologyMap()

	r.nodeCount = len(nodes.Items)

	if r.nodeCount < minNodes {
		return fmt.Errorf("Not enough nodes found: Expected %d, found %d", minNodes, r.nodeCount)
	}

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
			if strings.Contains(label, "rack") {
				if !nodeRacks.Contains(value, node.Name) {
					nodeRacks.Add(value, node.Name)
				}
			}
		}

	}

	if determineFailureDomain(sc) == "rack" {
		err = r.ensureNodeRacks(nodes, minNodes, nodeRacks, topologyMap, reqLogger)
		if err != nil {
			return err
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

// ensureNodeRacks iterates through the list of storage nodes and ensures
// all nodes have a rack topology label.
func (r *ReconcileStorageCluster) ensureNodeRacks(nodes *corev1.NodeList, minRacks int, nodeRacks, topologyMap *ocsv1.NodeTopologyMap, reqLogger logr.Logger) error {

	for _, node := range nodes.Items {
		hasRack := false

		for _, nodeNames := range nodeRacks.Labels {
			for _, nodeName := range nodeNames {
				if nodeName == node.Name {
					hasRack = true
					break
				}
			}
			if hasRack {
				break
			}
		}

		if !hasRack {
			rack := determinePlacementRack(nodes, node, minRacks, nodeRacks)
			nodeRacks.Add(rack, node.Name)
			if !topologyMap.Contains(defaults.RackTopologyKey, rack) {
				reqLogger.Info("Adding rack label from node", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
				topologyMap.Add(defaults.RackTopologyKey, rack)
			}

			reqLogger.Info("Labeling node with rack label", "Node", node.Name, "Label", defaults.RackTopologyKey, "Value", rack)
			newNode := node.DeepCopy()
			newNode.Labels[defaults.RackTopologyKey] = rack
			patch, err := generateStrategicPatch(node, newNode)
			if err != nil {
				return err
			}
			err = r.client.Patch(context.TODO(), &node, patch)
			if err != nil {
				return err
			}
		}
	}

	return nil
}

func generateStrategicPatch(oldObj, newObj interface{}) (client.Patch, error) {
	oldJSON, err := json.Marshal(oldObj)
	if err != nil {
		return nil, err
	}

	newJSON, err := json.Marshal(newObj)
	if err != nil {
		return nil, err
	}

	patch, err := strategicpatch.CreateTwoWayMergePatch(oldJSON, newJSON, oldObj)
	if err != nil {
		return nil, err
	}

	return client.ConstantPatch(types.StrategicMergePatchType, patch), nil
}

// determinePlacementRack sorts the list of known racks in alphabetical order,
// counts the number of Nodes in each rack, then returns the first rack with
// the fewest number of Nodes. If there are fewer than three racks, define new
// racks so that there are at least three. It also ensures that only racks with
// either no nodes or nodes in the same AZ are considered valid racks.
func determinePlacementRack(nodes *corev1.NodeList, node corev1.Node, minRacks int, nodeRacks *ocsv1.NodeTopologyMap) string {
	rackList := []string{}

	if len(nodeRacks.Labels) < minRacks {
		for i := len(nodeRacks.Labels); i < minRacks; i++ {
			for j := 0; j <= i; j++ {
				newRack := fmt.Sprintf("rack%d", j)
				if _, ok := nodeRacks.Labels[newRack]; !ok {
					nodeRacks.Labels[newRack] = ocsv1.TopologyLabelValues{}
					break
				}
			}
		}
	}

	targetAZ := ""
	for label, value := range node.Labels {
		for _, key := range validTopologyLabelKeys {
			if strings.Contains(label, key) && strings.Contains(label, "zone") {
				targetAZ = value
				break
			}
		}
		if targetAZ != "" {
			break
		}
	}

	if len(targetAZ) > 0 {
		for rack := range nodeRacks.Labels {
			nodeNames := nodeRacks.Labels[rack]
			if len(nodeNames) == 0 {
				rackList = append(rackList, rack)
				continue
			}

			validRack := false
			for _, nodeName := range nodeNames {
				for _, n := range nodes.Items {
					if n.Name == nodeName {
						for label, value := range n.Labels {
							for _, key := range validTopologyLabelKeys {
								if strings.Contains(label, key) && strings.Contains(label, "zone") && value == targetAZ {
									validRack = true
									break
								}
							}
							if validRack {
								break
							}
						}
						break
					}
				}
				if validRack {
					break
				}
			}
			if validRack {
				rackList = append(rackList, rack)
			}
		}
	} else {
		for rack := range nodeRacks.Labels {
			rackList = append(rackList, rack)
		}
	}

	sort.Strings(rackList)
	rack := rackList[0]

	for _, r := range rackList {
		if len(nodeRacks.Labels[r]) < len(nodeRacks.Labels[rack]) {
			rack = r
		}
	}

	return rack
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

// determineFailureDomain determines the appropriate Ceph failure domain based
// on the storage cluster's topology map
func determineFailureDomain(sc *ocsv1.StorageCluster) string {
	if sc.Status.FailureDomain != "" {
		return sc.Status.FailureDomain
	}
	topologyMap := sc.Status.NodeTopologies
	failureDomain := "rack"
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
				"all": defaults.DaemonPlacements["all"],
				"mon": getCephDaemonPlacements(sc, "mon"),
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
				in := defaults.DaemonPlacements["osd"]
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
			storageClassDeviceSets = append(storageClassDeviceSets, set)
		}
	}

	return storageClassDeviceSets
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

func (r *ReconcileStorageCluster) deleteResources(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (bool, error) {
	// NoobaaSystem is dependent upon ceph for volume provisioning.
	// We want to make sure we delete noobaasystem before we delete cephcluster, to get a clean uninstall.
	return r.deleteNoobaaSystems(sc, reqLogger)
}

//getCephDaemonPlacements returns placement configuration for ceph components with appropriate topology
func getCephDaemonPlacements(sc *ocsv1.StorageCluster, component string) rook.Placement {
	placement := rook.Placement{}
	in := defaults.DaemonPlacements[component]
	(&in).DeepCopyInto(&placement)
	topologyMap := sc.Status.NodeTopologies
	if topologyMap != nil {
		topologyKey := determineFailureDomain(sc)
		topologyKey, _ = topologyMap.GetKeyValues(topologyKey)
		podAffinityTerms := placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution
		podAffinityTerms[0].PodAffinityTerm.TopologyKey = topologyKey
	}

	return placement
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
				Name:     "FAILED_OSD_ID",
				Required: true,
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
		"app": "ceph-toolbox-job-${FAILED_OSD_ID}",
	}

	job := &batchv1.Job{
		TypeMeta: metav1.TypeMeta{
			Kind:       "Job",
			APIVersion: "batch/v1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ocs-osd-removal-${FAILED_OSD_ID}",
			Namespace: sc.Namespace,
			Labels:    labels,
		},
		Spec: batchv1.JobSpec{
			Template: corev1.PodTemplateSpec{
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "mon-endpoint-volume",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: "rook-ceph-mon-endpoints",
									},
									Items: []corev1.KeyToPath{
										{
											Key:  "data",
											Path: "mon-endpoints",
										},
									},
								},
							},
						},
						{
							Name:         "ceph-config",
							VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
						},
					},
					RestartPolicy: corev1.RestartPolicyNever,
					Containers: []corev1.Container{
						{
							Name:  "script",
							Image: os.Getenv("ROOK_CEPH_IMAGE"),
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/ceph",
									Name:      "ceph-config",
									ReadOnly:  true,
								},
							},
							Command: []string{"bash", "-c", "ceph osd out osd.${FAILED_OSD_ID};ceph osd purge osd.${FAILED_OSD_ID} --force --yes-i-really-mean-it"},
						},
					},
					InitContainers: []corev1.Container{
						{
							Name:            "config-init",
							Image:           os.Getenv("ROOK_CEPH_IMAGE"),
							Command:         []string{"/usr/local/bin/toolbox.sh"},
							Args:            []string{"--skip-watch"},
							ImagePullPolicy: corev1.PullIfNotPresent,
							VolumeMounts: []corev1.VolumeMount{
								{
									MountPath: "/etc/ceph",
									Name:      "ceph-config",
								},
								{
									Name:      "mon-endpoint-volume",
									MountPath: "/etc/rook",
								},
							},
							Env: []corev1.EnvVar{
								{
									Name: "ROOK_ADMIN_SECRET",
									ValueFrom: &corev1.EnvVarSource{
										SecretKeyRef: &corev1.SecretKeySelector{
											Key:                  "admin-secret",
											LocalObjectReference: corev1.LocalObjectReference{Name: "rook-ceph-mon"},
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

	return job
}
