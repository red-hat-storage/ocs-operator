package storagecluster

import (
	"context"
	"fmt"
	"testing"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/version"
)

func TestEnsureCephCluster(t *testing.T) {
	serverVersion := &version.Info{}
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
			c.cc = newCephCluster(mockStorageCluster, "", 3, serverVersion, log)
			c.cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
			if c.condition == "negativeCondition" {
				c.cc.Status.State = rookCephv1.ClusterStateCreated
			}
		}

		reconciler := createFakeStorageClusterReconciler(t, c.cc)
		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		err := reconciler.ensureCephCluster(sc, reconciler.reqLogger)
		assert.NoError(t, err)
		if c.condition == "" {
			expected := newCephCluster(sc, "", 3, reconciler.serverVersion, log)
			actual := newCephCluster(sc, "", 3, reconciler.serverVersion, log)
			err = reconciler.client.Get(context.TODO(), mockCephClusterNamespacedName, actual)
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
	serverVersion := &version.Info{}
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
		c.sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		actual := newCephCluster(c.sc, "", 3, serverVersion, log)
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
	sc1 := &api.StorageCluster{}
	sc1.Spec.StorageDeviceSets = mockDeviceSets
	sc1.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}

	sc2 := &api.StorageCluster{}
	sc2.Spec.Encryption.Enable = false
	sc2.Spec.StorageDeviceSets = mockDeviceSets
	sc2.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}

	sc3 := &api.StorageCluster{}
	sc3.Spec.Encryption.Enable = true
	sc3.Spec.StorageDeviceSets = mockDeviceSets
	sc3.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}

	sc4 := &api.StorageCluster{}
	sc4.Spec.StorageDeviceSets = mockDeviceSets
	sc4.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			defaults.RackTopologyKey: []string{
				"rack1",
				"rack2",
				"rack3",
			},
		},
	}
	var emptyLabelSelector = metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{},
	}
	sc3.Spec.LabelSelector = &emptyLabelSelector

	// Testing StorageClassDeviceSetCreation for kube version below 1.19
	serverVersion := &version.Info{
		Major: "1",
		Minor: "18",
	}
	cases := []struct {
		label                string
		sc                   *api.StorageCluster
		topologyKey          string
		lenOfMatchExpression int
	}{
		{
			label:       "case 1",
			sc:          sc1,
			topologyKey: "rack",
		},
		{
			label:                "case 2",
			sc:                   sc2,
			topologyKey:          "zone",
			lenOfMatchExpression: 2,
		},
		{
			label:                "case 3",
			sc:                   sc3,
			topologyKey:          "zone",
			lenOfMatchExpression: 1,
		},
	}

	for _, c := range cases {

		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		assert.Equal(t, defaults.DeviceSetReplica, len(actual))
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count/3, scds.Count)
			assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
			assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.Enable, scds.Encrypted)

			if c.topologyKey == "rack" {
				assert.Equal(t, getPlacement(c.sc, "osd"), scds.Placement)
			} else {
				topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
				assert.Equal(t, zoneTopologyLabel, topologyKey)
				matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
				assert.Equal(t, c.lenOfMatchExpression, len(matchExpressions))
				nodeSelector := matchExpressions[c.lenOfMatchExpression-1]
				assert.Equal(t, zoneTopologyLabel, nodeSelector.Key)
				assert.Equal(t, c.sc.Status.NodeTopologies.Labels[zoneTopologyLabel][i], nodeSelector.Values[0])
			}
		}

	}

	// Testing StorageClassDeviceSetCreation for kube version 1.19 and above
	serverVersion = &version.Info{
		Major: defaults.KubeMajorTopologySpreadConstraints,
		Minor: defaults.KubeMinorTopologySpreadConstraints,
	}
	cases = []struct {
		label                string
		sc                   *api.StorageCluster
		topologyKey          string
		lenOfMatchExpression int
	}{
		{
			label:                "case 1",
			sc:                   sc1,
			topologyKey:          "zone",
			lenOfMatchExpression: 1,
		},
		{
			label:                "case 2",
			sc:                   sc2,
			topologyKey:          "zone",
			lenOfMatchExpression: 1,
		},
		{
			label:                "case 3",
			sc:                   sc3,
			topologyKey:          "zone",
			lenOfMatchExpression: 0,
		},
		{
			label:                "case 4",
			sc:                   sc4,
			topologyKey:          "rack",
			lenOfMatchExpression: 1,
		},
	}

	for _, c := range cases {

		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		assert.Equal(t, defaults.DeviceSetReplica, len(actual))
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count/3, scds.Count)
			assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources)
			assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.Enable, scds.Encrypted)
			assert.Equal(t, getPlacement(c.sc, "osd-tsc"), scds.Placement)
			if c.lenOfMatchExpression == 0 {
				assert.Nil(t, scds.Placement.NodeAffinity)
			} else {
				matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
				assert.Equal(t, c.lenOfMatchExpression, len(matchExpressions))
			}
			topologyKey := scds.PreparePlacement.TopologySpreadConstraints[0].TopologyKey

			if c.topologyKey == "rack" {
				assert.Equal(t, defaults.RackTopologyKey, topologyKey)
			} else if len(c.sc.Status.NodeTopologies.Labels[zoneTopologyLabel]) >= defaults.DeviceSetReplica {
				assert.Equal(t, zoneTopologyLabel, topologyKey)
			} else {
				assert.Equal(t, hostnameLabel, topologyKey)
			}
		}

	}

}
