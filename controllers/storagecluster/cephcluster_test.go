package storagecluster

import (
	"context"
	"fmt"
	"testing"

	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
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
			c.cc = newCephCluster(mockStorageCluster, "", 3, serverVersion, nil, log)
			c.cc.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
			if c.condition == "negativeCondition" {
				c.cc.Status.State = rookCephv1.ClusterStateCreated
			}
		}

		var obj ocsCephCluster

		reconciler := createFakeStorageClusterReconciler(t, c.cc)
		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		err := obj.ensureCreated(&reconciler, sc)
		assert.NoError(t, err)
		if c.condition == "" {
			expected := newCephCluster(sc, "", 3, reconciler.serverVersion, nil, log)
			actual := newCephCluster(sc, "", 3, reconciler.serverVersion, nil, log)
			err = reconciler.Client.Get(context.TODO(), mockCephClusterNamespacedName, actual)
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

		actual := newCephCluster(c.sc, "", 3, serverVersion, nil, log)
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
			lenOfMatchExpression: 2,
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
			if scds.Portable && c.topologyKey == "rack" {
				assert.Equal(t, len(scds.Placement.TopologySpreadConstraints), 1)
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].TopologyKey, hostnameLabel)
				assert.Equal(t, getPlacement(c.sc, "osd-tsc").TopologySpreadConstraints, scds.Placement.TopologySpreadConstraints)
			} else {
				assert.Equal(t, getPlacement(c.sc, "osd-tsc"), scds.Placement)
			}
			if c.lenOfMatchExpression == 0 {
				assert.Nil(t, scds.Placement.NodeAffinity)
			} else {
				matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
				assert.Equal(t, c.lenOfMatchExpression, len(matchExpressions))
			}
			topologyKey := scds.PreparePlacement.TopologySpreadConstraints[0].TopologyKey

			if c.topologyKey == "rack" {
				// Node affinity is provided for distributing prepare pods across racks.
				// Host level topology spread is used to distribute evenly within rack.
				assert.Equal(t, hostnameLabel, topologyKey)
			} else if len(c.sc.Status.NodeTopologies.Labels[zoneTopologyLabel]) >= defaults.DeviceSetReplica {
				assert.Equal(t, zoneTopologyLabel, topologyKey)
			} else {
				assert.Equal(t, hostnameLabel, topologyKey)
			}
		}

	}

}

func createDummyKMSConfigMap(kmsProvider, kmsAddr string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	cm.Name = KMSConfigMapName
	cm.Data = make(map[string]string)
	cm.Data["KMS_PROVIDER"] = kmsProvider
	cm.Data[kmsProviderAddressKeyMap[kmsProvider]] = kmsAddr
	cm.Data["VAULT_BACKEND_PATH"] = "ocs"
	cm.Data["VAULT_NAMESPACE"] = "my-ocs-namespace"
	return cm
}

func TestKMSConfigChanges(t *testing.T) {
	validKMSArgs := []struct {
		testLabel       string
		kmsProvider     string
		kmsAddress      string
		failureExpected bool
	}{
		{testLabel: "case 1", kmsProvider: "vault", kmsAddress: "http://localhost:5050"},
		{testLabel: "case 2", kmsProvider: "vault", kmsAddress: "http://localhost:12321"},
		// ocs-operator is agnostic to KMS Provider, here rook should be throwing error
		{testLabel: "case 3", kmsProvider: "newKMSProvider", kmsAddress: "http://127.0.0.1:1553"},
		// invalid test cases, make sure label has a prefix 'invalid'
		{testLabel: "case 4", kmsProvider: "vault", kmsAddress: "http://unearchable.url.location:3366", failureExpected: true},
	}
	for _, kmsArgs := range validKMSArgs {
		assertCephClusterKMSConfiguration(t, kmsArgs)
	}
}

func assertCephClusterKMSConfiguration(t *testing.T, kmsArgs struct {
	testLabel       string
	kmsProvider     string
	kmsAddress      string
	failureExpected bool
}) {
	ctxTodo := context.TODO()
	kmsCM := createDummyKMSConfigMap(kmsArgs.kmsProvider, kmsArgs.kmsAddress)
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	if err := reconciler.Client.Create(ctxTodo, kmsCM); err != nil {
		t.Errorf("Unable to create KMS configmap: %v", err)
		t.FailNow()
	}
	// create a cephcluster CR and enable the KMS
	cr := createDefaultStorageCluster()
	cr.Spec.Encryption.KeyManagementService.Enable = true

	// don't start dummy servers for invalid tests
	if !kmsArgs.failureExpected {
		startServerAt(kmsArgs.kmsAddress)
	}

	var obj ocsCephCluster

	// have to initialize the image status,
	// without which the code will throw a 'nil pointer' exception
	reconciler.initializeImagesStatus(cr)
	err := obj.ensureCreated(&reconciler, cr)
	if kmsArgs.failureExpected && err == nil {
		// case 1: if a failure is expected and we don't receive any error
		t.Errorf("Failed: %q. Expected an error", kmsArgs.testLabel)
		t.FailNow()
	} else if !kmsArgs.failureExpected && err != nil {
		// case 2: if failure is not expected and we receive an error
		t.Errorf("Failed: %q. Error: %v", kmsArgs.testLabel, err)
		t.FailNow()
	} else if kmsArgs.failureExpected && err != nil {
		// case 3: if a failure was expected and we get the error
		// nothing to check further
		return
	}
	// following part of the tests are only for valid tests
	cephCluster := &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(ctxTodo,
		types.NamespacedName{Name: generateNameForCephCluster(cr)},
		cephCluster)
	if err != nil {
		t.Errorf("Get CephCluster Failed: %v", err)
		t.FailNow()
	}
	// check the provided KMS ConfigMap data is passed on to CephCluster
	for k, v := range kmsCM.Data {
		assert.Equal(t, v, cephCluster.Spec.Security.KeyManagementService.ConnectionDetails[k], fmt.Sprintf("Failed: %q. Expected values for key: %q, to be same", kmsArgs.testLabel, k))
	}
	assert.Equal(t, KMSTokenSecretName, cephCluster.Spec.Security.KeyManagementService.TokenSecretName, fmt.Sprintf("Failed: %q. Expected the token-names tobe same", kmsArgs.testLabel))
}
