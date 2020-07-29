package storagecluster

import (
	"fmt"
	"testing"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestEnsureCephCluster(t *testing.T) {
	// cases for testing
	cases := []struct {
		label     string
		cc        *rookCephv1.CephCluster
		isCreate  bool
		condition string
	}{
		{
			label:     "case 1", // create logic
			isCreate:  true,
			condition: "",
		},
		{
			label:     "case 2", // update logic
			isCreate:  false,
			condition: "",
		},
		{
			label:     "case 3", // No Conditions
			isCreate:  false,
			condition: "noCondition",
		},
		{
			label:     "case 4", // Negative Conditions
			isCreate:  false,
			condition: "negativeCondition",
		},
	}

	for _, c := range cases {
		c.cc = &rookCephv1.CephCluster{}
		if c.condition == "" {
			mockCephCluster.DeepCopyInto(c.cc)
			if c.isCreate {
				c.cc.ObjectMeta.Name = "doesn't exist"
			}
		} else {
			c.cc = newCephCluster(mockStorageCluster, "", 3, log)
			c.cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
			if c.condition == "negativeCondition" {
				c.cc.Status.State = rookCephv1.ClusterStateCreated
			}
		}

		reconciler := createFakeStorageClusterReconciler(t, c.cc)
		err := reconciler.ensureCephCluster(mockStorageCluster, reconciler.reqLogger)
		assert.NoError(t, err)
		if c.condition == "" {
			expected := newCephCluster(mockStorageCluster, "", 3, log)
			actual := newCephCluster(mockStorageCluster, "", 3, log)
			err = reconciler.client.Get(nil, mockCephClusterNamespacedName, actual)
			assert.NoError(t, err)
			assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
			assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
			assert.Equal(t, expected.Spec, actual.Spec)
		} else if c.condition == "noCondition" {

			assert.NotEmpty(t, reconciler.conditions)
			assert.Len(t, reconciler.conditions, 3)

			expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
				conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
				conditionsv1.ConditionProgressing: corev1.ConditionTrue,
				conditionsv1.ConditionUpgradeable: corev1.ConditionFalse,
			}
			for cType, status := range expectedConditions {
				found := assertCondition(reconciler.conditions, cType, status)
				assert.True(t, found, "expected status condition not found", cType, status)
			}

		} else {
			assert.Empty(t, reconciler.conditions)
		}

	}
}

func TestStorageClusterCephClusterCreation(t *testing.T) {
	// if both monPVCTemplate and monDataDirHostPath is provided via storageCluster
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	topologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{},
	}
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonDataDirHostPath = "/test/path"
	sc.Spec.MonPVCTemplate = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}}
	sc.Status.NodeTopologies = topologyMap
	actual := newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, actual.Spec.Mon.VolumeClaimTemplate.GetName(), sc.Spec.MonPVCTemplate.GetName())
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)

	// if only monDataDirHostPath is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonDataDirHostPath = "/test/path"
	sc.Status.NodeTopologies = topologyMap
	actual = newCephCluster(sc, "", 3, log)
	var emptyPVCSpec *corev1.PersistentVolumeClaim
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, emptyPVCSpec, actual.Spec.Mon.VolumeClaimTemplate)
	assert.Equal(t, "/test/path", actual.Spec.DataDirHostPath)

	// if only monPVCTemplate is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Spec.MonPVCTemplate = &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}}
	sc.Status.NodeTopologies = topologyMap
	actual = newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	assert.Equal(t, sc.Spec.MonPVCTemplate, actual.Spec.Mon.VolumeClaimTemplate)
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)

	// if no monPVCTemplate and no monDataDirHostPath is provided via storageCluster
	sc = &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Spec.StorageDeviceSets = mockDeviceSets
	sc.Status.NodeTopologies = topologyMap
	actual = newCephCluster(sc, "", 3, log)
	assert.Equal(t, generateNameForCephCluster(sc), actual.Name)
	assert.Equal(t, sc.Namespace, actual.Namespace)
	pvcSpec := actual.Spec.Mon.VolumeClaimTemplate.Spec
	assert.Equal(t, mockDeviceSets[0].DataPVCTemplate.Spec.StorageClassName, pvcSpec.StorageClassName)
	assert.Equal(t, "/var/lib/rook", actual.Spec.DataDirHostPath)
}

func TestStorageClassDeviceSetCreation(t *testing.T) {
	sc := &api.StorageCluster{}
	sc.Spec.StorageDeviceSets = mockDeviceSets

	nodeTopologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}
	sc.Status.NodeTopologies = nodeTopologyMap

	actual := newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	deviceSet := sc.Spec.StorageDeviceSets[0]
	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		assert.Equal(t, getPlacement(sc, "osd"), scds.Placement)
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}

	nodeTopologyMap.Labels[zoneTopologyLabel] = append(nodeTopologyMap.Labels[zoneTopologyLabel], "zone3")

	actual = newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
		assert.Equal(t, zoneTopologyLabel, topologyKey)
		matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		assert.Equal(t, 2, len(matchExpressions))
		nodeSelector := matchExpressions[1]
		assert.Equal(t, zoneTopologyLabel, nodeSelector.Key)
		assert.Equal(t, nodeTopologyMap.Labels[zoneTopologyLabel][i], nodeSelector.Values[0])
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}

	// Test with an empty label selector present in the StorageCluster.
	// This used to trigger a segfault (nil pointer dereference) in
	// newStorageClassDeviceSets. Make sure we don't regress.
	var emptyLabelSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{},
	}
	sc.Spec.LabelSelector = &emptyLabelSelector

	actual = newStorageClassDeviceSets(sc)
	assert.Equal(t, defaults.DeviceSetReplica, len(actual))

	for i, scds := range actual {
		assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
		// TODO: Change this when OCP console is updated
		assert.Equal(t, deviceSet.Count/3, scds.Count)
		assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
		topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
		assert.Equal(t, zoneTopologyLabel, topologyKey)
		matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
		assert.Equal(t, 1, len(matchExpressions))
		nodeSelector := matchExpressions[0]
		assert.Equal(t, zoneTopologyLabel, nodeSelector.Key)
		assert.Equal(t, nodeTopologyMap.Labels[zoneTopologyLabel][i], nodeSelector.Values[0])
		assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
		assert.Equal(t, true, scds.Portable)
	}
}
