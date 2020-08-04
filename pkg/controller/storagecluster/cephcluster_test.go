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

func TestNewCephClusterMonData(t *testing.T) {
	// if both monPVCTemplate and monDataDirHostPath is provided via storageCluster
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	topologyMap := &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{},
	}
	cases := []struct {
		label               string
		sc                  *api.StorageCluster
		monPVCTemplate      *corev1.PersistentVolumeClaim
		monDataPath         string
		expectedMonDataPath string
	}{
		{
			label:               "case 1", // both MonPvcTemplate and MonDataDirHostPath are provided via StorageCluster
			sc:                  sc,
			monPVCTemplate:      &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}},
			monDataPath:         "/test/path",
			expectedMonDataPath: "/var/lib/rook",
		},
		{
			label:               "case 2", // only MonDataDirHostPath is provided via StorageCluster
			sc:                  sc,
			monPVCTemplate:      nil,
			monDataPath:         "/test/path",
			expectedMonDataPath: "/test/path",
		},
		{
			label:               "case 3", // only MonPvcTemplate is provided via StorageCluster
			sc:                  sc,
			monPVCTemplate:      &corev1.PersistentVolumeClaim{ObjectMeta: metav1.ObjectMeta{Name: "test-mon-PVC"}},
			monDataPath:         "",
			expectedMonDataPath: "/var/lib/rook",
		},
		{
			label:               "case 4", // no MonPvcTemplate and no MonDataDirHostPath are provided via StorageCluster
			sc:                  sc,
			monPVCTemplate:      nil,
			monDataPath:         "",
			expectedMonDataPath: "/var/lib/rook",
		},
	}

	for _, c := range cases {
		mockStorageCluster.DeepCopyInto(c.sc)
		c.sc.Spec.StorageDeviceSets = mockDeviceSets
		c.sc.Status.NodeTopologies = topologyMap
		c.sc.Spec.MonPVCTemplate = c.monPVCTemplate
		c.sc.Spec.MonDataDirHostPath = c.monDataPath

		actual := newCephCluster(c.sc, "", 3, log)
		assert.Equal(t, generateNameForCephCluster(c.sc), actual.Name)
		assert.Equal(t, c.sc.Namespace, actual.Namespace)
		assert.Equal(t, c.expectedMonDataPath, actual.Spec.DataDirHostPath)

		if c.monPVCTemplate != nil {
			assert.Equal(t, actual.Spec.Mon.VolumeClaimTemplate, c.sc.Spec.MonPVCTemplate)
		} else {
			if c.monDataPath != "" {
				var emptyPVCSpec *corev1.PersistentVolumeClaim
				assert.Equal(t, emptyPVCSpec, actual.Spec.Mon.VolumeClaimTemplate)
			} else {
				pvcSpec := actual.Spec.Mon.VolumeClaimTemplate.Spec
				assert.Equal(t, mockDeviceSets[0].DataPVCTemplate.Spec.StorageClassName, pvcSpec.StorageClassName)
			}
		}

	}
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
