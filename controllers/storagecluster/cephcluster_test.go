package storagecluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/openshift/ocs-operator/controllers/defaults"
	ocsutil "github.com/openshift/ocs-operator/controllers/util"

	"github.com/google/go-cmp/cmp"
	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	v1 "github.com/openshift/api/config/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/openshift/ocs-operator/api/v1"
	ocsv1 "github.com/openshift/ocs-operator/api/v1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
)

func TestEnsureCephCluster(t *testing.T) {
	// cases for testing
	cases := []struct {
		label            string
		shouldCreate     bool
		cephClusterState cephv1.ClusterState
		reconcilerPhase  string
	}{
		{
			label:            "Create new CephCluster",
			shouldCreate:     true,
			cephClusterState: "",
		},
		{
			label:            "Reconcile CephCluster not reporting state",
			cephClusterState: "",
		},
		{
			label:            "Reconcile creating CephCluster",
			cephClusterState: cephv1.ClusterStateCreating,
		},
		{
			label:            "Reconcile updating CephCluster",
			cephClusterState: cephv1.ClusterStateUpdating,
		},
		{
			label:            "Reconcile degraded CephCluster",
			cephClusterState: cephv1.ClusterStateError,
		},
		{
			label:            "CephCluster reconciled succesfully",
			cephClusterState: cephv1.ClusterStateCreated,
		},
		{
			label:            "Update expanding CephCluster",
			cephClusterState: cephv1.ClusterStateUpdating,
			reconcilerPhase:  ocsutil.PhaseClusterExpanding,
		},
	}

	for i, c := range cases {
		t.Logf("Case %d: %s\n", i+1, c.label)

		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t)

		expected := newCephCluster(mockStorageCluster, "", 3, reconciler.serverVersion, nil, log)
		expected.ObjectMeta.SelfLink = "/api/v1/namespaces/ceph/secrets/pvc-ceph-client-key"
		expected.Status.State = c.cephClusterState

		if !c.shouldCreate {
			createErr := reconciler.Client.Create(context.TODO(), expected)
			assert.NoError(t, createErr)
		}

		// To test for cluster expansion, the expected CephCluster must
		// have more more storage devices defined than the existing
		// CephCluster.
		if c.reconcilerPhase == ocsutil.PhaseClusterExpanding {
			createErr := reconciler.Client.Create(context.TODO(), fakeStorageClass)
			assert.NoError(t, createErr)

			sc.Spec.StorageDeviceSets = []api.StorageDeviceSet{
				{
					Name:    "mock-sds",
					Count:   3,
					Replica: 1,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &fakeStorageClassName,
						},
					},
				},
			}
			sc.Spec.MonDataDirHostPath = "/var/lib/rook"
			expected.Spec.Storage.StorageClassDeviceSets = newStorageClassDeviceSets(sc, reconciler.serverVersion)
		}

		var obj ocsCephCluster
		err := obj.ensureCreated(&reconciler, sc)
		assert.NoError(t, err)

		actual := &cephv1.CephCluster{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}, actual)
		assert.NoError(t, err)
		assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
		assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
		assert.Equal(t, expected.Spec, actual.Spec)

		expectedConditions := []conditionsv1.Condition{}
		if c.cephClusterState == "" {
			ocsutil.MapCephClusterNoConditions(&expectedConditions, "", "")
		} else {
			ocsutil.MapCephClusterNegativeConditions(&expectedConditions, expected)
		}

		assert.Len(t, reconciler.conditions, len(expectedConditions))
		for i, condition := range expectedConditions {
			if i < len(reconciler.conditions) {
				assert.Equal(t, condition.Type, reconciler.conditions[i].Type)
				assert.Equal(t, condition.Status, reconciler.conditions[i].Status)
			}
		}
	}
}

func TestCephClusterMonTimeout(t *testing.T) {
	// cases for testing
	cases := []struct {
		label    string
		platform v1.PlatformType
	}{
		{
			label:    "case 1", // when the platform is not identified
			platform: v1.NonePlatformType,
		},
		{
			label:    "case 2", // when platform is IBMCloudPlatformType
			platform: v1.IBMCloudPlatformType,
		},
		{
			label:    "case 3", // when platform is IBMCloudCosPlatformType
			platform: IBMCloudCosPlatformType,
		},
	}

	for _, c := range cases {
		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t, mockCephCluster)

		reconciler.platform = &Platform{
			platform: c.platform,
		}

		var obj ocsCephCluster
		err := obj.ensureCreated(&reconciler, sc)
		assert.NoError(t, err)

		cc := newCephCluster(sc, "", 3, reconciler.serverVersion, nil, log)
		err = reconciler.Client.Get(context.TODO(), mockCephClusterNamespacedName, cc)
		assert.NoError(t, err)
		if c.platform == v1.IBMCloudPlatformType || c.platform == IBMCloudCosPlatformType {
			assert.Equal(t, "15m", cc.Spec.HealthCheck.DaemonHealth.Monitor.Timeout)
		} else {
			assert.Equal(t, "", cc.Spec.HealthCheck.DaemonHealth.Monitor.Timeout)
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
			if scds.Portable && c.topologyKey == "rack" {
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].WhenUnsatisfiable, corev1.UnsatisfiableConstraintAction("DoNotSchedule"))
				assert.Equal(t, len(scds.Placement.TopologySpreadConstraints), 2)
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].TopologyKey, defaults.RackTopologyKey)
				placementOsd := getPlacement(c.sc, "osd-tsc")
				newTSC := placementOsd.TopologySpreadConstraints[0]
				newTSC.TopologyKey = defaults.RackTopologyKey
				newTSC.WhenUnsatisfiable = corev1.UnsatisfiableConstraintAction("DoNotSchedule")
				placementOsd.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{newTSC, placementOsd.TopologySpreadConstraints[0]}
				assert.Equal(t, placementOsd, scds.Placement)
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
				assert.Equal(t, defaults.RackTopologyKey, topologyKey)
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
	cephCluster := &cephv1.CephCluster{}
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

func TestStorageClassDeviceSetCreationForArbiter(t *testing.T) {

	sc1 := &api.StorageCluster{}
	sc1.Spec.Arbiter.Enable = true
	sc1.Spec.NodeTopologies = &api.NodeTopologyMap{
		ArbiterLocation: "zone3",
	}
	sc1.Spec.StorageDeviceSets = getMockDeviceSets("mock", 1, 4, true)
	sc1.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels: map[string]api.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
		ArbiterLocation: "zone3",
	}
	sc1.Status.FailureDomain = "zone"
	sc1.Status.FailureDomainKey = zoneTopologyLabel
	sc1.Status.FailureDomainValues = []string{"zone1", "zone2"}

	sc2 := &api.StorageCluster{}
	sc2.Spec.Arbiter.Enable = true
	sc2.Spec.NodeTopologies = &api.NodeTopologyMap{
		ArbiterLocation: "zone3",
	}
	sc2.Spec.StorageDeviceSets = getMockDeviceSets("mock", 1, 6, true)
	sc2.Status.NodeTopologies = &api.NodeTopologyMap{
		Labels:          map[string]api.TopologyLabelValues{zoneTopologyLabel: []string{"zone1", "zone2"}},
		ArbiterLocation: "zone3",
	}
	sc2.Status.FailureDomain = "zone"
	sc2.Status.FailureDomainKey = zoneTopologyLabel
	sc2.Status.FailureDomainValues = []string{"zone1", "zone2"}

	serverVersion := &version.Info{
		Major: defaults.KubeMajorTopologySpreadConstraints,
		Minor: defaults.KubeMinorTopologySpreadConstraints,
	}
	cases := []struct {
		label       string
		sc          *api.StorageCluster
		topologyKey string
	}{
		{
			label:       "case 1",
			sc:          sc1,
			topologyKey: zoneTopologyLabel,
		},
		{
			label:       "case 2",
			sc:          sc2,
			topologyKey: zoneTopologyLabel,
		},
	}

	for _, c := range cases {

		setFailureDomain(c.sc)
		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name, c.label)
			assert.Equal(t, deviceSet.Count, scds.Count, c.label)
			assert.Equal(t, defaults.DaemonResources["osd"], scds.Resources, c.label)
			assert.Equal(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0], c.label)
			assert.Equal(t, true, scds.Portable, c.label)
			assert.Equal(t, c.sc.Spec.Encryption.Enable, scds.Encrypted, c.label)
			assert.Equal(t, getPlacement(c.sc, "osd-tsc"), scds.Placement, c.label)
			topologyKey := scds.PreparePlacement.TopologySpreadConstraints[0].TopologyKey
			assert.Equal(t, c.topologyKey, topologyKey, c.label)
		}

	}

}

func TestNewCephDaemonResources(t *testing.T) {

	cases := []struct {
		name     string
		spec     *api.StorageCluster
		expected map[string]corev1.ResourceRequirements
	}{
		{
			name: "When nothing is passed to StorageCluster.Spec.Resources (Defaults)",
			spec: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mon": defaults.DaemonResources["mon"],
				"mgr": defaults.DaemonResources["mgr"],
				"mds": defaults.DaemonResources["mds"],
				"rgw": defaults.DaemonResources["rgw"],
			},
		},
		{
			name: "Overriding defaults",
			spec: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mds": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
						},
					},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mon": defaults.DaemonResources["mon"],
				"mgr": defaults.DaemonResources["mgr"],
				"mds": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				"rgw": defaults.DaemonResources["rgw"],
			},
		},
		{
			name: "Passing a new key",
			spec: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"crashcollector": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
							Limits: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("6"),
								corev1.ResourceMemory: resource.MustParse("16Gi"),
							},
						},
					},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mon": defaults.DaemonResources["mon"],
				"mgr": defaults.DaemonResources["mgr"],
				"mds": defaults.DaemonResources["mds"],
				"rgw": defaults.DaemonResources["rgw"],
				"crashcollector": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
			},
		},
		{
			name: "When nothing is passed to StorageCluster.Spec.Resources (Defaults) and arbiter is enabled",
			spec: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{},
					Arbiter: api.ArbiterSpec{
						Enable: true,
					},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mon":         defaults.DaemonResources["mon"],
				"mgr":         defaults.DaemonResources["mgr"],
				"mds":         defaults.DaemonResources["mds"],
				"rgw":         defaults.DaemonResources["rgw"],
				"mgr-sidecar": defaults.DaemonResources["mgr-sidecar"],
			},
		},
	}

	for _, c := range cases {
		got := newCephDaemonResources(c.spec)
		assert.Equalf(t, len(c.expected), len(got), c.name)
		assert.Equalf(t, c.expected, got, c.name)
	}
}

func TestGetNetworkSpec(t *testing.T) {
	testTable := []struct {
		desc     string
		scSpec   ocsv1.StorageClusterSpec
		expected cephv1.NetworkSpec
	}{
		{
			desc: "hostNetwork specified as true, network unspecified",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: true,
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: true,
			},
		},
		{
			desc: "hostNetwork specified as false, network unspecified",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: false,
			},
		},
		{
			desc: "hostNetwork specified as true, network specified without hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: true,
				Network: &cephv1.NetworkSpec{
					HostNetwork: false, // same as default
					IPFamily:    cephv1.IPv6,
				},
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    cephv1.IPv6,
			},
		},
		{
			desc: "hostNetwork specified as false, network specified with hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
				Network: &cephv1.NetworkSpec{
					HostNetwork: true,
					IPFamily:    cephv1.IPv6,
					DualStack:   true,
				},
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    cephv1.IPv6,
				DualStack:   true,
			},
		},
		{
			desc: "hostNetwork specified as false, network specified without hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
				Network: &cephv1.NetworkSpec{
					HostNetwork: false,
					IPFamily:    cephv1.IPv4,
				},
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: false,
				IPFamily:    cephv1.IPv4,
			},
		},
		{
			desc:     "hostNetwork unspecified, network unspecified",
			scSpec:   ocsv1.StorageClusterSpec{},
			expected: cephv1.NetworkSpec{},
		},
		{
			desc: "hostNetwork unspecified, network specified",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &cephv1.NetworkSpec{
					HostNetwork: true,
					IPFamily:    cephv1.IPv4,
				},
			},
			expected: cephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    cephv1.IPv4,
			},
		},
	}
	for _, testCase := range testTable {
		t.Logf("running testCase %q", testCase.desc)
		sc := ocsv1.StorageCluster{
			Spec: testCase.scSpec,
		}
		actual := getNetworkSpec(sc)
		// test Provider, Selectors, HostNetwork, IPFamily, Dualstack
		assert.Truef(t, cmp.Equal(testCase.expected, actual), "diff between expected(-) and actual(+) not empty: %s", cmp.Diff(testCase.expected, actual))

	}
}
