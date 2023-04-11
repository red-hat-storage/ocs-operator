package storagecluster

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	v1 "github.com/openshift/api/config/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/defaults"
	ocsutil "github.com/red-hat-storage/ocs-operator/controllers/util"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/version"
)

func TestEnsureCephCluster(t *testing.T) {
	// cases for testing
	testSkipPrometheusRules = true
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
			label:            "CephCluster reconciled successfully",
			cephClusterState: cephv1.ClusterStateCreated,
		},
		{
			label:            "Update expanding CephCluster",
			cephClusterState: cephv1.ClusterStateUpdating,
			reconcilerPhase:  ocsutil.PhaseClusterExpanding,
		},
	}

	k := 1
	for i, c := range cases {
		k++
		t.Logf("Case %d: %s\n", i+1, c.label)

		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t)

		expected, err := newCephCluster(mockStorageCluster.DeepCopy(), "", 3, reconciler.serverVersion, nil, log)
		assert.NilError(t, err)
		expected.Status.State = c.cephClusterState

		if !c.shouldCreate {
			createErr := reconciler.Client.Create(context.TODO(), expected)
			assert.NilError(t, createErr)
		}

		// To test for cluster expansion, the expected CephCluster must
		// have more more storage devices defined than the existing
		// CephCluster.
		if c.reconcilerPhase == ocsutil.PhaseClusterExpanding {
			createErr := reconciler.Client.Create(context.TODO(), fakeStorageClass)
			assert.NilError(t, createErr)

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
		_, err = obj.ensureCreated(&reconciler, sc)
		assert.NilError(t, err)

		actual := &cephv1.CephCluster{}
		err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: expected.Name, Namespace: expected.Namespace}, actual)
		assert.NilError(t, err)
		assert.Equal(t, expected.ObjectMeta.Name, actual.ObjectMeta.Name)
		assert.Equal(t, expected.ObjectMeta.Namespace, actual.ObjectMeta.Namespace)
		assert.DeepEqual(t, expected.Spec, actual.Spec)

		expectedConditions := []conditionsv1.Condition{}
		if c.cephClusterState == "" {
			ocsutil.MapCephClusterNoConditions(&expectedConditions, "", "")
		} else {
			ocsutil.MapCephClusterNegativeConditions(&expectedConditions, expected)
		}

		assert.Assert(t, is.Len(reconciler.conditions, len(expectedConditions)))
		for i, condition := range expectedConditions {
			if i < len(reconciler.conditions) {
				assert.Equal(t, condition.Type, reconciler.conditions[i].Type)
				assert.Equal(t, condition.Status, reconciler.conditions[i].Status)
			}
		}
	}
	{
		t.Logf("Case %d: %s\n", k, "Unreachable KMS error handling")
		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Encryption.Enable = true
		sc.Spec.Encryption.KeyManagementService.Enable = true
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}
		KMSConfigMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      KMSConfigMapName,
				Namespace: sc.Namespace,
			},
			Data: map[string]string{
				"KMS_PROVIDER": "vault",
				"VAULT_ADDR":   "http://vault.example.com:9000",
			},
		}

		reconciler := createFakeStorageClusterReconciler(t, KMSConfigMap)

		var obj ocsCephCluster
		_, err := obj.ensureCreated(&reconciler, sc)
		assert.Equal(t, sc.Status.KMSServerConnection.KMSServerAddress, KMSConfigMap.Data["VAULT_ADDR"])
		assert.Equal(t, sc.Status.KMSServerConnection.KMSServerConnectionError, err.Error())
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
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		sc := &api.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t, mockCephCluster.DeepCopy())

		reconciler.platform = &Platform{
			platform: c.platform,
		}

		var obj ocsCephCluster
		_, err := obj.ensureCreated(&reconciler, sc)
		assert.NilError(t, err)

		cc, err := newCephCluster(sc, "", 3, reconciler.serverVersion, nil, log)
		assert.NilError(t, err)
		err = reconciler.Client.Get(context.TODO(), mockCephClusterNamespacedName, cc)
		assert.NilError(t, err)
		if c.platform == v1.IBMCloudPlatformType {
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
		t.Logf("Case: %s\n", c.label)
		mockStorageCluster.DeepCopyInto(c.sc)
		c.sc.Spec.StorageDeviceSets = mockDeviceSets
		c.sc.Status.NodeTopologies = topologyMap
		c.sc.Spec.MonPVCTemplate = c.monPVCTemplate
		c.sc.Spec.MonDataDirHostPath = c.monDataPath
		c.sc.Status.Images.Ceph = &api.ComponentImageStatus{}

		actual, err := newCephCluster(c.sc, "", 3, serverVersion, nil, log)
		assert.NilError(t, err)
		assert.Equal(t, generateNameForCephCluster(c.sc), actual.Name)
		assert.Equal(t, c.sc.Namespace, actual.Namespace)
		assert.Equal(t, c.expectedMonDataPath, actual.Spec.DataDirHostPath)

		if c.monPVCTemplate != nil {
			assert.DeepEqual(t, actual.Spec.Mon.VolumeClaimTemplate, c.sc.Spec.MonPVCTemplate)
		} else {
			if c.monDataPath != "" {
				var emptyPVCSpec *corev1.PersistentVolumeClaim
				assert.DeepEqual(t, emptyPVCSpec, actual.Spec.Mon.VolumeClaimTemplate)
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
	sc2.Spec.Encryption.ClusterWide = false
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
	sc3.Spec.Encryption.ClusterWide = true
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
		t.Logf("Case: %s\n", c.label)
		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		assert.Equal(t, defaults.DeviceSetReplica, len(actual))
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count/3, scds.Count)
			assert.DeepEqual(t, defaults.DaemonResources["osd"], scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)

			if c.topologyKey == "rack" {
				assert.DeepEqual(t, getPlacement(c.sc, "osd"), scds.Placement)
			} else {
				topologyKey := scds.Placement.PodAntiAffinity.PreferredDuringSchedulingIgnoredDuringExecution[0].PodAffinityTerm.TopologyKey
				assert.Equal(t, zoneTopologyLabel, topologyKey)
				matchExpressions := scds.Placement.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution.NodeSelectorTerms[0].MatchExpressions
				assert.Assert(t, is.Len(matchExpressions, c.lenOfMatchExpression))
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
		t.Logf("Case: %s\n", c.label)
		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		assert.Equal(t, defaults.DeviceSetReplica, len(actual))
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count/3, scds.Count)
			assert.DeepEqual(t, defaults.DaemonResources["osd"], scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)
			if scds.Portable && c.topologyKey == "rack" {
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].WhenUnsatisfiable, corev1.UnsatisfiableConstraintAction("DoNotSchedule"))
				assert.Equal(t, len(scds.Placement.TopologySpreadConstraints), 2)
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].TopologyKey, defaults.RackTopologyKey)
				placementOsd := getPlacement(c.sc, "osd-tsc")
				newTSC := placementOsd.TopologySpreadConstraints[0]
				newTSC.TopologyKey = defaults.RackTopologyKey
				newTSC.WhenUnsatisfiable = corev1.UnsatisfiableConstraintAction("DoNotSchedule")
				placementOsd.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{newTSC, placementOsd.TopologySpreadConstraints[0]}
				assert.DeepEqual(t, placementOsd, scds.Placement)
			} else {
				assert.DeepEqual(t, getPlacement(c.sc, "osd-tsc"), scds.Placement)
			}
			if c.lenOfMatchExpression == 0 {
				assert.Assert(t, is.Nil(scds.Placement.NodeAffinity))
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

func createDummyKMSConfigMap(kmsProvider, kmsAddr string, kmsAuthMethod string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	cm.Name = KMSConfigMapName
	cm.Data = make(map[string]string)
	cm.Data["KMS_PROVIDER"] = kmsProvider
	cm.Data["KMS_SERVICE_NAME"] = "my-connection"

	switch kmsProvider {
	case VaultKMSProvider:
		if kmsAuthMethod == VaultSAAuthMethod {
			cm.Data["VAULT_AUTH_METHOD"] = VaultSAAuthMethod
		}
		cm.Data[kmsProviderAddressKeyMap[kmsProvider]] = kmsAddr
		cm.Data["VAULT_BACKEND_PATH"] = "ocs"
		cm.Data["VAULT_NAMESPACE"] = "my-ocs-namespace"
	case IbmKeyProtectKMSProvider:
		cm.Data["IBM_KP_SERVICE_INSTANCE_ID"] = "my-instance-id"
		cm.Data["IBM_KP_SECRET_NAME"] = "my-kms-key"
		cm.Data["IBM_KP_BASE_URL"] = "my-base-url"
		cm.Data["IBM_KP_TOKEN_URL"] = "my-token-url"
	}
	return cm
}

func TestKMSConfigChanges(t *testing.T) {
	validKMSArgs := []struct {
		testLabel             string
		kmsProvider           string
		kmsAddress            string
		enabled               bool
		clusterWideEncryption bool
		failureExpected       bool
		authMethod            string
	}{
		{testLabel: "case 1", kmsProvider: VaultKMSProvider,
			clusterWideEncryption: true, kmsAddress: "http://localhost:5050", authMethod: VaultTokenAuthMethod},
		{testLabel: "case 2", kmsProvider: VaultKMSProvider,
			clusterWideEncryption: true, kmsAddress: "http://localhost:12321", authMethod: VaultTokenAuthMethod},
		// ocs-operator is agnostic to KMS Provider, here rook should be throwing error
		{testLabel: "case 3", kmsProvider: "newKMSProvider",
			clusterWideEncryption: true, kmsAddress: "http://127.0.0.1:1553"},
		// invalid test cases, make sure label has a prefix 'invalid'
		{testLabel: "case 4", kmsProvider: VaultKMSProvider,
			clusterWideEncryption: true, kmsAddress: "http://unearchable.url.location:3366", failureExpected: true, authMethod: VaultTokenAuthMethod},
		{testLabel: "case 5", kmsProvider: VaultKMSProvider,
			clusterWideEncryption: true, kmsAddress: "http://localhost:1234", authMethod: VaultSAAuthMethod},
		{testLabel: "case 6", kmsProvider: IbmKeyProtectKMSProvider,
			clusterWideEncryption: true, kmsAddress: ""},
		// backward compatible test
		{testLabel: "case 7", kmsProvider: VaultKMSProvider,
			enabled: true, kmsAddress: "http://localhost:5678", authMethod: VaultSAAuthMethod},
		{testLabel: "case 8", kmsProvider: ThalesKMSProvider,
			clusterWideEncryption: true, kmsAddress: "http://localhost:5671"},
	}
	for _, kmsArgs := range validKMSArgs {
		t.Run(kmsArgs.testLabel, func(t *testing.T) {
			assertCephClusterKMSConfiguration(t, kmsArgs)
		})
	}
}

func assertCephClusterKMSConfiguration(t *testing.T, kmsArgs struct {
	testLabel             string
	kmsProvider           string
	kmsAddress            string
	enabled               bool
	clusterWideEncryption bool
	failureExpected       bool
	authMethod            string
}) {
	ctxTodo := context.TODO()
	kmsCM := createDummyKMSConfigMap(kmsArgs.kmsProvider, kmsArgs.kmsAddress, kmsArgs.authMethod)
	reconciler := createFakeInitializationStorageClusterReconciler(t, &nbv1.NooBaa{})
	if err := reconciler.Client.Create(ctxTodo, kmsCM); err != nil {
		t.Errorf("Unable to create KMS configmap: %v", err)
		t.FailNow()
	}
	// create a cephcluster CR and enable the KMS
	cr := createDefaultStorageCluster()
	cr.Spec.Encryption.KeyManagementService.Enable = true
	cr.Spec.Encryption.ClusterWide = kmsArgs.clusterWideEncryption
	cr.Spec.Encryption.Enable = kmsArgs.enabled

	// don't start dummy servers for invalid tests
	if !kmsArgs.failureExpected || kmsArgs.kmsAddress != "" {
		startServerAt(t, kmsArgs.kmsAddress)
	}

	var obj ocsCephCluster

	// have to initialize the image status,
	// without which the code will throw a 'nil pointer' exception
	reconciler.initializeImagesStatus(cr)
	_, err := obj.ensureCreated(&reconciler, cr)
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
	if kmsArgs.enabled || kmsArgs.clusterWideEncryption {
		for k, v := range kmsCM.Data {
			assert.Equal(t, v, cephCluster.Spec.Security.KeyManagementService.ConnectionDetails[k], "Failed: %q. Expected values for key: %q, to be same", kmsArgs.testLabel, k)
		}
		if kmsArgs.authMethod == VaultTokenAuthMethod {
			assert.Equal(t, KMSTokenSecretName, cephCluster.Spec.Security.KeyManagementService.TokenSecretName, "Failed: %q. Expected the token-names tobe same", kmsArgs.testLabel)
		} else if kmsArgs.kmsProvider == IbmKeyProtectKMSProvider || kmsArgs.kmsProvider == ThalesKMSProvider {
			assert.Equal(t, kmsCM.Data[kmsProviderSecretKeyMap[kmsArgs.kmsProvider]], cephCluster.Spec.Security.KeyManagementService.TokenSecretName, "Failed: %q. Expected the token-names tobe same", kmsArgs.testLabel)
		}
	}
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
		t.Logf("Case: %s\n", c.label)

		setFailureDomain(c.sc)
		actual := newStorageClassDeviceSets(c.sc, serverVersion)
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count, scds.Count)
			assert.DeepEqual(t, defaults.DaemonResources["osd"], scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate, scds.VolumeClaimTemplates[0])
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)
			assert.DeepEqual(t, getPlacement(c.sc, "osd-tsc"), scds.Placement)
			topologyKey := scds.PreparePlacement.TopologySpreadConstraints[0].TopologyKey
			assert.Equal(t, c.topologyKey, topologyKey)
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
		t.Logf("Case: %s\n", c.name)
		got := newCephDaemonResources(c.spec)
		assert.DeepEqual(t, c.expected, got)
	}
}

func TestParsePrometheusRules(t *testing.T) {
	prometheusRules, err := parsePrometheusRule(localPrometheusRules)
	assert.NilError(t, err)
	assert.Equal(t, 11, len(prometheusRules.Spec.Groups))

	prometheusRules, err = parsePrometheusRule(externalPrometheusRules)
	assert.NilError(t, err)
	assert.Equal(t, 1, len(prometheusRules.Spec.Groups))
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
		t.Logf("Case: %q\n", testCase.desc)
		sc := ocsv1.StorageCluster{
			Spec: testCase.scSpec,
		}
		actual := getNetworkSpec(sc)
		// test Provider, Selectors, HostNetwork, IPFamily, Dualstack
		assert.DeepEqual(t, actual, testCase.expected)

	}
}

func TestGetCephClusterMonitoringLabels(t *testing.T) {

	type args struct {
		sc ocsv1.StorageCluster
	}
	tests := []struct {
		name string
		args args
		want map[string]string
	}{
		{
			name: "Default StorageCluster, No Labels",
			args: args{
				sc: ocsv1.StorageCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "storagecluster",
					},
					Spec: ocsv1.StorageClusterSpec{
						Monitoring: nil,
					},
				},
			},
			want: map[string]string{
				"rook.io/managedBy": "storagecluster",
			},
		},
		{
			name: "StorageCluster with labels",
			args: args{
				sc: ocsv1.StorageCluster{
					ObjectMeta: metav1.ObjectMeta{
						Name: "storagecluster",
					},
					Spec: ocsv1.StorageClusterSpec{
						Monitoring: &ocsv1.MonitoringSpec{
							Labels: map[string]string{
								"dummyKey": "dummyValue",
							},
						},
					},
				},
			},
			want: map[string]string{
				"rook.io/managedBy": "storagecluster",
				"dummyKey":          "dummyValue",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := getCephClusterMonitoringLabels(tt.args.sc); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("getCephClusterMonitoringLabels() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLogCollector(t *testing.T) {
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	maxLogSize, err := resource.ParseQuantity("500Mi")
	assert.NilError(t, err)

	defaultLogCollector := cephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	sc.Spec.LogCollector = &defaultLogCollector

	r := createFakeStorageClusterReconciler(t)
	actual, err := newCephCluster(sc, "", 3, r.serverVersion, nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	// when disabled in storageCluster
	sc.Spec.LogCollector = &cephv1.LogCollectorSpec{}
	actual, err = newCephCluster(sc, "", 3, r.serverVersion, nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	maxLogSize, err = resource.ParseQuantity("6Gi")
	assert.NilError(t, err)
	sc.Spec.LogCollector.MaxLogSize = &maxLogSize

	actual, err = newCephCluster(sc, "", 3, r.serverVersion, nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector.MaxLogSize, &maxLogSize)
}
