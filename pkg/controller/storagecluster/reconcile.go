package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/blang/semver"
	"github.com/go-logr/logr"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	statusutil "github.com/openshift/ocs-operator/pkg/controller/util"
	"github.com/openshift/ocs-operator/version"
	"github.com/operator-framework/operator-sdk/pkg/ready"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// StorageClassProvisionerType is a string representing StorageClass Provisioner. E.g: aws-ebs
type StorageClassProvisionerType string

// CleanupPolicyType is a string representing cleanup policy
type CleanupPolicyType string

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
	// CleanupPolicyLabel defines the cleanup policy
	CleanupPolicyLabel = "cleanup.ocs.openshift.io"
	// CleanupPolicyDelete when set, modifies the cleanup policy for Rook to delete the DataDirHostPath on uninstall
	CleanupPolicyDelete CleanupPolicyType = "yes-really-destroy-data"
	//Name of MetadataPVCTemplate
	metadataPVCName = "metadata"
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
			err = r.deleteResources(instance, reqLogger)
			if err != nil {
				return reconcile.Result{}, err
			}
			reqLogger.Info("Removing finalizer")
			// Once all finalizers have been removed, the object will be deleted
			instance.ObjectMeta.Finalizers = remove(instance.ObjectMeta.Finalizers, storageClusterFinalizer)
			if err := r.client.Update(context.TODO(), instance); err != nil {
				reqLogger.Error(err, "Failed to remove finalizer from storagecluster")
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
		if err := r.ensurestorageclusterinit(instance, request, reqLogger); err != nil {
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
			r.ensureExternalStorageClusterResources,
			r.ensureCephCluster,
			r.ensureNoobaaSystem,
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
	}

	return nil
}

func (r *ReconcileStorageCluster) getStorageClusterEligibleNodes(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (nodes *corev1.NodeList, err error) {
	nodes = &corev1.NodeList{}
	var selector labels.Selector

	labelSelector := &metav1.LabelSelector{
		MatchLabels: map[string]string{defaults.NodeAffinityKey: ""},
	}
	if sc.Spec.LabelSelector != nil {
		labelSelector = sc.Spec.LabelSelector
	}

	selector, err = metav1.LabelSelectorAsSelector(labelSelector)
	err = r.client.List(context.TODO(), nodes, MatchingLabelsSelector{Selector: selector})

	return nodes, err
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

	nodes, err := r.getStorageClusterEligibleNodes(sc, reqLogger)
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

func (r *ReconcileStorageCluster) setRookCleanupPolicy(instance *ocsv1.StorageCluster, reqLogger logr.Logger) (err error) {
	if v, found := instance.ObjectMeta.Labels[CleanupPolicyLabel]; found {
		if v == string(CleanupPolicyDelete) {
			cephCluster := &cephv1.CephCluster{}
			err = r.client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephCluster(instance), Namespace: instance.Namespace}, cephCluster)
			if err != nil {
				if errors.IsNotFound(err) {
					reqLogger.Info("CephCluster not found, can't set the cleanup policy")
				} else {
					return fmt.Errorf("Unable to retrive cephCluster: %v", err)
				}
			} else {
				cephCluster.Spec.CleanupPolicy.Confirmation = cephv1.DeleteDataDirOnHostsConfirmation
				err := r.client.Update(context.TODO(), cephCluster)
				if err != nil {
					return fmt.Errorf("Unable to update cephCluster: %v", err)
				}
			}
		}
	}
	return nil
}

// deleteResources is the function where the storageClusterFinalizer is handled
// Every function that is called within this function
// 1. should be idempotent
// 2. should wrap the finalerr with its own err
// 3. optionally set a state variable for other calls which might depend on it
// If this function returns a nil finalerr, the finalizer is removed.
func (r *ReconcileStorageCluster) deleteResources(sc *ocsv1.StorageCluster, reqLogger logr.Logger) (finalerr error) {

	err := r.setRookCleanupPolicy(sc, reqLogger)
	if err != nil {
		finalerr = fmt.Errorf("%w, %v", finalerr, err)
	}
	// NoobaaSystem is dependent upon ceph for volume provisioning.
	// We want to make sure we delete noobaasystem before we delete cephcluster, to get a clean uninstall.
	_, err = r.deleteNoobaaSystems(sc, reqLogger)
	if err != nil {
		finalerr = fmt.Errorf("%w, %v", finalerr, err)
	}

	err = r.deleteStorageClasses(sc, reqLogger)
	if err != nil {
		finalerr = fmt.Errorf("%w, %v", finalerr, err)
	}

	err = r.deleteNodeAffinityKeyFromNodes(sc, reqLogger)
	if err != nil {
		finalerr = fmt.Errorf("%w, %v", finalerr, err)
	}

	err = r.deleteNodeTaint(sc, reqLogger)
	if err != nil {
		finalerr = fmt.Errorf("%w, %v", finalerr, err)
	}

	return finalerr
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

	// The purgeOSDScript finds osd status for given FAILED_OSD_ID whether it's up or down. The action will be taken according to osd status. If osd is up and running, it won't be marked out. If osd is down it can be taken out of the cluster and purged.
	const purgeOSDScript = `
set -x

osd_status=$(ceph osd tree | grep "osd.${FAILED_OSD_ID} " | awk '{print $5}')
if [[ "$osd_status" == "up" ]]; then
echo "OSD ${FAILED_OSD_ID} is up and running."
echo "Please check if you entered correct ID of failed osd!"
else
echo "OSD ${FAILED_OSD_ID} is down. Proceeding to mark out and purge"
ceph osd out osd.${FAILED_OSD_ID}
ceph osd purge osd.${FAILED_OSD_ID} --force --yes-i-really-mean-it
fi`

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
							Command: []string{
								"/bin/bash",
								"-c",
								purgeOSDScript,
							},
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
