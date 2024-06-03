package storagecluster

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	ocsutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

var networkConfig = &configv1.Network{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "cluster",
		Namespace: "",
	},
	Status: configv1.NetworkStatus{
		ClusterNetwork: []configv1.ClusterNetworkEntry{
			{CIDR: "198.1v2.3.4/16"},
		},
	},
}

func TestEnsureCephCluster(t *testing.T) {
	// cases for testing
	testSkipPrometheusRules = true
	cases := []struct {
		label            string
		shouldCreate     bool
		cephClusterState rookCephv1.ClusterState
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
			cephClusterState: rookCephv1.ClusterStateCreating,
		},
		{
			label:            "Reconcile updating CephCluster",
			cephClusterState: rookCephv1.ClusterStateUpdating,
		},
		{
			label:            "Reconcile degraded CephCluster",
			cephClusterState: rookCephv1.ClusterStateError,
		},
		{
			label:            "CephCluster reconciled successfully",
			cephClusterState: rookCephv1.ClusterStateCreated,
		},
		{
			label:            "Update expanding CephCluster",
			cephClusterState: rookCephv1.ClusterStateUpdating,
			reconcilerPhase:  ocsutil.PhaseClusterExpanding,
		},
	}

	k := 1
	for i, c := range cases {
		k++
		t.Logf("Case %d: %s\n", i+1, c.label)

		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t, networkConfig)

		expected, err := newCephCluster(mockStorageCluster.DeepCopy(), "", nil, log)
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

			sc.Spec.StorageDeviceSets = []ocsv1.StorageDeviceSet{
				{
					Name:        "mock-sds",
					Count:       3,
					DeviceClass: "HDD",
					Replica:     1,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &fakeStorageClassName,
						},
					},
				},
			}
			sc.Spec.MonDataDirHostPath = "/var/lib/rook"
			expected.Spec.Storage.StorageClassDeviceSets = newStorageClassDeviceSets(sc)
		}

		var obj ocsCephCluster
		_, err = obj.ensureCreated(&reconciler, sc)
		assert.NilError(t, err)

		actual := &rookCephv1.CephCluster{}
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
		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Encryption.Enable = true
		sc.Spec.Encryption.KeyManagementService.Enable = true
		sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
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
		platform configv1.PlatformType
	}{
		{
			label:    "case 1", // when the platform is not identified
			platform: configv1.NonePlatformType,
		},
		{
			label:    "case 2", // when platform is IBMCloudPlatformType
			platform: configv1.IBMCloudPlatformType,
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		platform.SetFakePlatformInstanceForTesting(true, c.platform)

		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}

		reconciler := createFakeStorageClusterReconciler(t, mockCephCluster.DeepCopy(), networkConfig)
		var obj ocsCephCluster
		_, err := obj.ensureCreated(&reconciler, sc)
		assert.NilError(t, err)

		cc, err := newCephCluster(sc, "", nil, log)
		assert.NilError(t, err)
		err = reconciler.Client.Get(context.TODO(), mockCephClusterNamespacedName, cc)
		assert.NilError(t, err)
		if c.platform == configv1.IBMCloudPlatformType {
			assert.Equal(t, "15m", cc.Spec.HealthCheck.DaemonHealth.Monitor.Timeout)
		} else {
			assert.Equal(t, "", cc.Spec.HealthCheck.DaemonHealth.Monitor.Timeout)
		}

		platform.UnsetFakePlatformInstanceForTesting()
	}
}

func TestNewCephClusterMonData(t *testing.T) {
	// if both monPVCTemplate and monDataDirHostPath is provided via storageCluster
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	topologyMap := &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{},
	}
	cases := []struct {
		label               string
		sc                  *ocsv1.StorageCluster
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
		c.sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}

		actual, err := newCephCluster(c.sc, "", nil, log)
		assert.NilError(t, err)
		assert.Equal(t, generateNameForCephCluster(c.sc), actual.Name)
		assert.Equal(t, c.sc.Namespace, actual.Namespace)
		assert.Equal(t, c.expectedMonDataPath, actual.Spec.DataDirHostPath)

		if c.monPVCTemplate != nil {
			assert.DeepEqual(t, actual.Spec.Mon.VolumeClaimTemplate.ObjectMeta, c.sc.Spec.MonPVCTemplate.ObjectMeta)
			assert.DeepEqual(t, actual.Spec.Mon.VolumeClaimTemplate.Spec, c.sc.Spec.MonPVCTemplate.Spec)
		} else {
			if c.monDataPath != "" {
				var emptyPVCSpec *rookCephv1.VolumeClaimTemplate
				assert.DeepEqual(t, emptyPVCSpec, actual.Spec.Mon.VolumeClaimTemplate)
			} else {
				pvcSpec := actual.Spec.Mon.VolumeClaimTemplate.Spec
				assert.Equal(t, mockDeviceSets[0].DataPVCTemplate.Spec.StorageClassName, pvcSpec.StorageClassName)
			}
		}

	}
}

func TestGenerateMgrSpec(t *testing.T) {
	cases := []struct {
		label        string
		sc           *ocsv1.StorageCluster
		isSingleNode bool
		expectedMgr  rookCephv1.MgrSpec
	}{
		{
			label: "Default case",
			sc:    &ocsv1.StorageCluster{},
			expectedMgr: rookCephv1.MgrSpec{
				Count:                defaults.DefaultMgrCount,
				AllowMultiplePerNode: false,
				Modules: []rookCephv1.Module{
					{Name: "pg_autoscaler", Enabled: true},
					{Name: "balancer", Enabled: true, Settings: rookCephv1.ModuleSettings{BalancerMode: "upmap-read"}},
				},
			},
		},
		{
			label: "MgrCount is set to 1 on the storageCluster CR Spec",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							MgrCount: 1,
						},
					},
				},
			},
			expectedMgr: rookCephv1.MgrSpec{
				Count:                1,
				AllowMultiplePerNode: false,
				Modules: []rookCephv1.Module{
					{Name: "pg_autoscaler", Enabled: true},
					{Name: "balancer", Enabled: true, Settings: rookCephv1.ModuleSettings{BalancerMode: "upmap-read"}},
				},
			},
		},
		{
			label: "MgrCount is set to 1 on the storageCluster CR Spec & it's arbiter mode",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Arbiter: ocsv1.ArbiterSpec{
						Enable: true,
					},
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							MgrCount: 1,
						},
					},
				},
			},
			expectedMgr: rookCephv1.MgrSpec{
				Count:                2,
				AllowMultiplePerNode: false,
				Modules: []rookCephv1.Module{
					{Name: "pg_autoscaler", Enabled: true},
					{Name: "balancer", Enabled: true, Settings: rookCephv1.ModuleSettings{BalancerMode: "upmap-read"}},
				},
			},
		},
		{
			label:        "Single node deployment",
			sc:           &ocsv1.StorageCluster{},
			isSingleNode: true,
			expectedMgr: rookCephv1.MgrSpec{
				Count:                1,
				AllowMultiplePerNode: true,
				Modules: []rookCephv1.Module{
					{Name: "pg_autoscaler", Enabled: true},
					{Name: "balancer", Enabled: true, Settings: rookCephv1.ModuleSettings{BalancerMode: "upmap-read"}},
				},
			},
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		t.Setenv(ocsutil.SingleNodeEnvVar, strconv.FormatBool(c.isSingleNode))
		actual := generateMgrSpec(c.sc)
		assert.DeepEqual(t, c.expectedMgr, actual)
	}
}

func TestGenerateMonSpec(t *testing.T) {
	arbiterSc := &ocsv1.StorageCluster{
		Spec: ocsv1.StorageClusterSpec{
			Arbiter: ocsv1.ArbiterSpec{
				Enable: true,
			},
			NodeTopologies: &ocsv1.NodeTopologyMap{
				ArbiterLocation: "zone3",
			},
		},
		Status: ocsv1.StorageClusterStatus{
			NodeTopologies: &ocsv1.NodeTopologyMap{
				Labels: map[string]ocsv1.TopologyLabelValues{
					zoneTopologyLabel: []string{
						"zone1",
						"zone2",
					},
				},
				ArbiterLocation: "zone3",
			},
			FailureDomain:       "zone",
			FailureDomainKey:    zoneTopologyLabel,
			FailureDomainValues: []string{"zone1", "zone2"},
		},
	}

	cases := []struct {
		label        string
		sc           *ocsv1.StorageCluster
		isSingleNode bool
		expectedMon  rookCephv1.MonSpec
	}{
		{
			label: "Default case",
			sc:    &ocsv1.StorageCluster{},
			expectedMon: rookCephv1.MonSpec{
				Count:                defaults.DefaultMonCount,
				AllowMultiplePerNode: false,
			},
		},
		{
			label: "MonCount is set to 5 on the storageCluster CR Spec",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							MonCount: 5,
						},
					},
				},
			},
			expectedMon: rookCephv1.MonSpec{
				Count:                5,
				AllowMultiplePerNode: false,
			},
		},
		{
			label: "Arbiter Mode",
			sc:    arbiterSc,
			expectedMon: rookCephv1.MonSpec{
				Count:                defaults.ArbiterModeMonCount,
				AllowMultiplePerNode: false,
				StretchCluster:       generateStretchClusterSpec(arbiterSc),
			},
		},
		{
			label: "Arbiter Mode with MonCount set to 3 on the storageCluster CR Spec",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							MonCount: 3,
						},
					},
					Arbiter:        arbiterSc.Spec.Arbiter,
					NodeTopologies: arbiterSc.Spec.NodeTopologies,
				},
				Status: arbiterSc.Status,
			},
			expectedMon: rookCephv1.MonSpec{
				Count:                5,
				AllowMultiplePerNode: false,
				StretchCluster:       generateStretchClusterSpec(arbiterSc),
			},
		},
		{
			label:        "Single node deployment",
			sc:           &ocsv1.StorageCluster{},
			isSingleNode: true,
			expectedMon: rookCephv1.MonSpec{
				Count:                3,
				AllowMultiplePerNode: true,
			},
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		t.Setenv(ocsutil.SingleNodeEnvVar, strconv.FormatBool(c.isSingleNode))
		actual := generateMonSpec(c.sc)
		assert.DeepEqual(t, c.expectedMon, actual)
	}

}

func TestStorageClassDeviceSetCreation(t *testing.T) {
	sc1 := &ocsv1.StorageCluster{}
	sc1.Spec.StorageDeviceSets = mockDeviceSets
	sc1.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
			},
		},
	}

	sc2 := &ocsv1.StorageCluster{}
	sc2.Spec.Encryption.ClusterWide = false
	sc2.Spec.StorageDeviceSets = mockDeviceSets
	sc2.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}

	sc3 := &ocsv1.StorageCluster{}
	sc3.Spec.Encryption.ClusterWide = true
	sc3.Spec.StorageDeviceSets = mockDeviceSets
	sc3.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{
			zoneTopologyLabel: []string{
				"zone1",
				"zone2",
				"zone3",
			},
		},
	}

	sc4 := &ocsv1.StorageCluster{}
	sc4.Spec.StorageDeviceSets = mockDeviceSets
	sc4.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{
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

	cases := []struct {
		label                string
		sc                   *ocsv1.StorageCluster
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
		actual := newStorageClassDeviceSets(c.sc)
		assert.Equal(t, defaults.DeviceSetReplica, len(actual))
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count/3, scds.Count)
			assert.DeepEqual(t, defaults.GetProfileDaemonResources("osd", c.sc), scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.ObjectMeta, scds.VolumeClaimTemplates[0].ObjectMeta)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.Spec, scds.VolumeClaimTemplates[0].Spec)
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)
			if scds.Portable && c.topologyKey == "rack" {
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].WhenUnsatisfiable, corev1.UnsatisfiableConstraintAction("DoNotSchedule"))
				assert.Equal(t, len(scds.Placement.TopologySpreadConstraints), 2)
				assert.Equal(t, scds.Placement.TopologySpreadConstraints[0].TopologyKey, defaults.RackTopologyKey)
				placementOsd := getPlacement(c.sc, "osd")
				newTSC := placementOsd.TopologySpreadConstraints[0]
				newTSC.TopologyKey = defaults.RackTopologyKey
				newTSC.WhenUnsatisfiable = corev1.UnsatisfiableConstraintAction("DoNotSchedule")
				placementOsd.TopologySpreadConstraints = []corev1.TopologySpreadConstraint{newTSC, placementOsd.TopologySpreadConstraints[0]}
				assert.DeepEqual(t, placementOsd, scds.Placement)
			} else {
				assert.DeepEqual(t, getPlacement(c.sc, "osd"), scds.Placement)
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
	case AzureKSMProvider:
		cm.Data["AZURE_CLIENT_ID"] = "azure-client-id"
		cm.Data["AZURE_TENANT_ID"] = "azure-tenant-id"
		cm.Data["AZURE_VAULT_URL"] = kmsAddr
		cm.Data["AZURE_CERT_SECRET_NAME"] = "cert-secret"
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
		{testLabel: "case 9", kmsProvider: AzureKSMProvider,
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
	cephCluster := &rookCephv1.CephCluster{}
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

	sc1 := &ocsv1.StorageCluster{}
	sc1.Spec.Arbiter.Enable = true
	sc1.Spec.NodeTopologies = &ocsv1.NodeTopologyMap{
		ArbiterLocation: "zone3",
	}
	sc1.Spec.StorageDeviceSets = getMockDeviceSets("mock", 1, 4, true)
	sc1.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels: map[string]ocsv1.TopologyLabelValues{
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

	sc2 := &ocsv1.StorageCluster{}
	sc2.Spec.Arbiter.Enable = true
	sc2.Spec.NodeTopologies = &ocsv1.NodeTopologyMap{
		ArbiterLocation: "zone3",
	}
	sc2.Spec.StorageDeviceSets = getMockDeviceSets("mock", 1, 6, true)
	sc2.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels:          map[string]ocsv1.TopologyLabelValues{zoneTopologyLabel: []string{"zone1", "zone2"}},
		ArbiterLocation: "zone3",
	}
	sc2.Status.FailureDomain = "zone"
	sc2.Status.FailureDomainKey = zoneTopologyLabel
	sc2.Status.FailureDomainValues = []string{"zone1", "zone2"}

	cases := []struct {
		label       string
		sc          *ocsv1.StorageCluster
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
		actual := newStorageClassDeviceSets(c.sc)
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count, scds.Count)
			assert.DeepEqual(t, defaults.GetProfileDaemonResources("osd", c.sc), scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.ObjectMeta, scds.VolumeClaimTemplates[0].ObjectMeta)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.Spec, scds.VolumeClaimTemplates[0].Spec)
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)
			assert.DeepEqual(t, getPlacement(c.sc, "osd"), scds.Placement)
			topologyKey := scds.PreparePlacement.TopologySpreadConstraints[0].TopologyKey
			assert.Equal(t, c.topologyKey, topologyKey)
		}

	}

}

func TestNewCephDaemonResources(t *testing.T) {

	cases := []struct {
		name     string
		sc       *ocsv1.StorageCluster
		expected map[string]corev1.ResourceRequirements
	}{
		{
			name: "No ResourceRequirements are set & No ResourceProfile is set",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": defaults.BalancedDaemonResources["mgr"],
				"mon": defaults.BalancedDaemonResources["mon"],
			},
		},
		{
			name: "No ResourceRequirements are set & ResourceProfile is `lean`",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources:       map[string]corev1.ResourceRequirements{},
					ResourceProfile: "lean",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": defaults.LeanDaemonResources["mgr"],
				"mon": defaults.LeanDaemonResources["mon"],
			},
		},
		{
			name: "No ResourceRequirements are set & ResourceProfile is `balanced`",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources:       map[string]corev1.ResourceRequirements{},
					ResourceProfile: "balanced",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": defaults.BalancedDaemonResources["mgr"],
				"mon": defaults.BalancedDaemonResources["mon"],
			},
		},
		{
			name: "No ResourceRequirements are set & ResourceProfile is `performance`",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources:       map[string]corev1.ResourceRequirements{},
					ResourceProfile: "performance",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": defaults.PerformanceDaemonResources["mgr"],
				"mon": defaults.PerformanceDaemonResources["mon"],
			},
		},
		{
			name: "Some ResourceRequirements are passed & ResourceProfile is not set",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mon": {
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
				"mgr": defaults.BalancedDaemonResources["mgr"],
				"mon": {
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
			name: "Some ResourceRequirements are passed & ResourceProfile is also set",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mgr": {
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
					ResourceProfile: "performance",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("6"),
						corev1.ResourceMemory: resource.MustParse("16Gi"),
					},
				},
				"mon": defaults.PerformanceDaemonResources["mon"],
			},
		},
		{
			name: "Some new custom ResourceRequirements are passed & ResourceProfile is not set",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
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
				"mon": defaults.BalancedDaemonResources["mon"],
				"mgr": defaults.BalancedDaemonResources["mgr"],
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
			name: "Some new custom ResourceRequirements are passed & ResourceProfile is also set",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
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
					ResourceProfile: "lean",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": defaults.LeanDaemonResources["mgr"],
				"mon": defaults.LeanDaemonResources["mon"],
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
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.name)
		got := newCephDaemonResources(c.sc)
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

func TestChangePrometheusExprFunc(t *testing.T) {
	prometheusRules, err := parsePrometheusRule(localPrometheusRules)
	assert.NilError(t, err)
	var changeTokens = []exprReplaceToken{
		{recordOrAlertName: "CephMgrIsAbsent", wordInExpr: "openshift-storage", replaceWith: "new-namespace"},
		// when alert or record name is not specified,
		// the change should affect all the expressions which has the 'wordInExpr'
		{recordOrAlertName: "", wordInExpr: "ceph_pool_stored_raw", replaceWith: "new_ceph_pool_stored_raw"},
	}
	changePromRuleExpr(prometheusRules, changeTokens)
	alertNameAndChangedExpr := [][2]string{
		{"CephMgrIsAbsent", "new-namespace"},
		{"CephPoolQuotaBytesNearExhaustion", "new_ceph_pool_stored_raw"},
		{"CephPoolQuotaBytesCriticallyExhausted", "new_ceph_pool_stored_raw"},
	}
	for _, grp := range prometheusRules.Spec.Groups {
		for _, rule := range grp.Rules {
			for _, eachAlertChanged := range alertNameAndChangedExpr {
				alertName := eachAlertChanged[0]
				changeStr := eachAlertChanged[1]
				if rule.Alert != alertName {
					continue
				}
				assert.Assert(t, strings.Contains(rule.Expr.String(), changeStr))
			}
		}
	}
}

func TestGetNetworkSpec(t *testing.T) {
	testTable := []struct {
		desc     string
		scSpec   ocsv1.StorageClusterSpec
		expected rookCephv1.NetworkSpec
	}{
		{
			desc: "hostNetwork specified as true, network unspecified",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: true,
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: true,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc: "hostNetwork specified as false, network unspecified",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: false,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc: "hostNetwork specified as true, network specified without hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: true,
				Network: &rookCephv1.NetworkSpec{
					HostNetwork: false, // same as default
					IPFamily:    rookCephv1.IPv6,
				},
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    rookCephv1.IPv6,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc: "hostNetwork specified as false, network specified with hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
				Network: &rookCephv1.NetworkSpec{
					HostNetwork: true,
					IPFamily:    rookCephv1.IPv6,
					DualStack:   true,
				},
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    rookCephv1.IPv6,
				DualStack:   true,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc: "hostNetwork specified as false, network specified without hostnetwork",
			scSpec: ocsv1.StorageClusterSpec{
				HostNetwork: false,
				Network: &rookCephv1.NetworkSpec{
					HostNetwork: false,
					IPFamily:    rookCephv1.IPv4,
				},
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: false,
				IPFamily:    rookCephv1.IPv4,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc:   "hostNetwork unspecified, network unspecified",
			scSpec: ocsv1.StorageClusterSpec{},
			expected: rookCephv1.NetworkSpec{
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
			},
		},
		{
			desc: "hostNetwork unspecified, network specified",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					HostNetwork: true,
					IPFamily:    rookCephv1.IPv4,
				},
			},
			expected: rookCephv1.NetworkSpec{
				HostNetwork: true,
				IPFamily:    rookCephv1.IPv4,
				Connections: &rookCephv1.ConnectionsSpec{
					RequireMsgr2: true,
				},
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
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	maxLogSize, err := resource.ParseQuantity("500Mi")
	assert.NilError(t, err)

	defaultLogCollector := rookCephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	sc.Spec.LogCollector = &defaultLogCollector

	actual, err := newCephCluster(sc, "", nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	// when disabled in storageCluster
	sc.Spec.LogCollector = &rookCephv1.LogCollectorSpec{}
	actual, err = newCephCluster(sc, "", nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	maxLogSize, err = resource.ParseQuantity("6Gi")
	assert.NilError(t, err)
	sc.Spec.LogCollector.MaxLogSize = &maxLogSize

	actual, err = newCephCluster(sc, "", nil, log)
	assert.NilError(t, err)
	assert.DeepEqual(t, actual.Spec.LogCollector.MaxLogSize, &maxLogSize)
}

func TestCephClusterNetworkConnectionsSpec(t *testing.T) {
	testTable := []struct {
		desc   string
		scSpec ocsv1.StorageClusterSpec
		ccSpec rookCephv1.ClusterSpec
	}{
		{
			desc: "No Network Connections Spec is defined in StorageCluster",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{},
				},
			},
			ccSpec: rookCephv1.ClusterSpec{
				Network: rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{},
				},
			},
		},
		{
			desc: "Encryption Enabled is true",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: true,
						},
					},
				},
			},
			ccSpec: rookCephv1.ClusterSpec{
				Network: rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: true,
						},
					},
				},
			},
		},
		{
			desc: "Compression is enabled",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Compression: &rookCephv1.CompressionSpec{
							Enabled: true,
						},
					},
				},
			},
			ccSpec: rookCephv1.ClusterSpec{
				Network: rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Compression: &rookCephv1.CompressionSpec{
							Enabled: true,
						},
					},
				},
			},
		},
		{
			desc: "Encryption Enabled is true, Compression Enabled is true",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: true,
						},
						Compression: &rookCephv1.CompressionSpec{
							Enabled: true,
						},
					},
				},
			},
			ccSpec: rookCephv1.ClusterSpec{
				Network: rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: true,
						},
						Compression: &rookCephv1.CompressionSpec{
							Enabled: true,
						},
					},
				},
			},
		},
		{
			desc: "Encryption Enabled is false, Compression Enabled is false",
			scSpec: ocsv1.StorageClusterSpec{
				Network: &rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: false,
						},
						Compression: &rookCephv1.CompressionSpec{
							Enabled: false,
						},
					},
				},
			},
			ccSpec: rookCephv1.ClusterSpec{
				Network: rookCephv1.NetworkSpec{
					Connections: &rookCephv1.ConnectionsSpec{
						Encryption: &rookCephv1.EncryptionSpec{
							Enabled: false,
						},
						Compression: &rookCephv1.CompressionSpec{
							Enabled: false,
						},
					},
				},
			},
		},
	}
	// Test for external mode
	for _, testCase := range testTable {
		t.Logf("Test for external mode")
		t.Logf("Case: %q\n", testCase.desc)
		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Network = testCase.scSpec.Network
		sc.Spec.ExternalStorage.Enable = true
		cc := newExternalCephCluster(sc, "", "")
		assert.DeepEqual(t, cc.Spec.Network.Connections, testCase.ccSpec.Network.Connections)
	}
	// Test for internal mode
	for _, testCase := range testTable {
		t.Logf("Test for internal mode")
		t.Logf("Case: %q\n", testCase.desc)
		sc := &ocsv1.StorageCluster{}
		mockStorageCluster.DeepCopyInto(sc)
		sc.Spec.Network = testCase.scSpec.Network
		testCase.ccSpec.Network.Connections.RequireMsgr2 = true
		cc, _ := newCephCluster(sc, "", nil, log)
		assert.DeepEqual(t, cc.Spec.Network.Connections, testCase.ccSpec.Network.Connections)
	}
}

func TestGetIPFamilyConfig(t *testing.T) {
	testTable := []struct {
		label string
		// status of the configv1.Network object for the cluster
		networkStatus    configv1.NetworkStatus
		expectedIPFamily rookCephv1.IPFamilyType
		isDualStack      bool
		expectedError    error
	}{
		{
			label: "Case #1: DualStack cluster",
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
					{CIDR: "fd01::/48"},
				},
			},
			expectedIPFamily: "",
			isDualStack:      true,
			expectedError:    nil,
		},
		{
			label: "Case #2: IPv6 Single Stack Cluster",
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "fd01::/48"},
				},
			},
			expectedIPFamily: "IPv6",
			isDualStack:      false,
			expectedError:    nil,
		},
		{
			label: "Case #1: IPv4 cluster",
			networkStatus: configv1.NetworkStatus{
				ClusterNetwork: []configv1.ClusterNetworkEntry{
					{CIDR: "198.1v2.3.4/16"},
				},
			},
			expectedIPFamily: "IPv4",
			isDualStack:      false,
			expectedError:    nil,
		},
	}

	for _, tc := range testTable {
		t.Run(tc.label, func(t *testing.T) {
			networkConfig := &configv1.Network{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster",
					Namespace: "",
				},
				Status: tc.networkStatus,
			}
			r := createFakeStorageClusterReconciler(t, networkConfig)
			ipfamily, isDualStack, err := getIPFamilyConfig(r.Client)
			assert.Equal(t, ipfamily, tc.expectedIPFamily)
			assert.Equal(t, isDualStack, tc.isDualStack)
			assert.Equal(t, err, tc.expectedError)
		})

	}
}

func TestCephClusterStoreType(t *testing.T) {
	sc := &ocsv1.StorageCluster{}

	t.Run("ensure no bluestore optimization", func(t *testing.T) {
		actual, err := newCephCluster(sc, "", nil, log)
		assert.NilError(t, err)
		assert.Equal(t, "", actual.Spec.Storage.Store.Type)
	})

	t.Run("ensure bluestore optimization based on annotation for internal clusters", func(t *testing.T) {
		annotations := map[string]string{
			DisasterRecoveryTargetAnnotation: "true",
		}
		sc.Annotations = annotations
		actual, err := newCephCluster(sc, "", nil, log)
		assert.NilError(t, err)
		assert.Equal(t, "bluestore-rdr", actual.Spec.Storage.Store.Type)
	})

	t.Run("ensure no bluestore optimization for external clusters", func(t *testing.T) {
		sc.Spec.ExternalStorage.Enable = true
		actual, err := newCephCluster(sc, "", nil, log)
		assert.NilError(t, err)
		assert.Equal(t, "", actual.Spec.Storage.Store.Type)
	})
}

func TestEnsureRDROptmizations(t *testing.T) {
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
	sc.Annotations[DisasterRecoveryTargetAnnotation] = "true"
	reconciler := createFakeStorageClusterReconciler(t, networkConfig)

	// Ensure bluestore-rdr store type if RDR optimization annotation is added
	var obj ocsCephCluster
	_, err := obj.ensureCreated(&reconciler, sc)
	assert.NilError(t, err)
	actual := &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephClusterFromString(sc.Name), Namespace: sc.Namespace}, actual)
	assert.NilError(t, err)
	assert.Equal(t, string(rookCephv1.StoreTypeBlueStoreRDR), actual.Spec.Storage.Store.Type)

	// Ensure bluestoreRDR store is not overridden if required annotations are removed later on
	testSkipPrometheusRules = true
	delete(sc.Annotations, DisasterRecoveryTargetAnnotation)
	_, err = obj.ensureCreated(&reconciler, sc)
	assert.NilError(t, err)
	actual = &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephClusterFromString(sc.Name), Namespace: sc.Namespace}, actual)
	assert.NilError(t, err)
	assert.Equal(t, string(rookCephv1.StoreTypeBlueStoreRDR), actual.Spec.Storage.Store.Type)
}

func TestEnsureRDRMigration(t *testing.T) {
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
	reconciler := createFakeStorageClusterReconciler(t, networkConfig)

	// Ensure bluestore store type if RDR optimization annotation is not added
	var obj ocsCephCluster
	_, err := obj.ensureCreated(&reconciler, sc)
	assert.NilError(t, err)
	actual := &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephClusterFromString(sc.Name), Namespace: sc.Namespace}, actual)
	assert.NilError(t, err)
	assert.Equal(t, "", actual.Spec.Storage.Store.Type)
	assert.Equal(t, "", actual.Spec.Storage.Store.UpdateStore)

	// Ensure bluestoreRDR migration is set if RDR optimization annotation is added later on
	testSkipPrometheusRules = true
	sc.Annotations[DisasterRecoveryTargetAnnotation] = "true"
	_, err = obj.ensureCreated(&reconciler, sc)
	assert.NilError(t, err)
	actual = &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: generateNameForCephClusterFromString(sc.Name), Namespace: sc.Namespace}, actual)
	assert.NilError(t, err)
	assert.Equal(t, string(rookCephv1.StoreTypeBlueStoreRDR), actual.Spec.Storage.Store.Type)
	assert.Equal(t, "yes-really-update-store", actual.Spec.Storage.Store.UpdateStore)
}

func TestEnsureUpgradeReliabilityParams(t *testing.T) {
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
	defaultContinueUpgradeAfterChecksEvenIfNotHealthyVal := true
	sc.Spec.ManagedResources.CephCluster.ContinueUpgradeAfterChecksEvenIfNotHealthy = &defaultContinueUpgradeAfterChecksEvenIfNotHealthyVal
	sc.Spec.ManagedResources.CephCluster.SkipUpgradeChecks = true
	sc.Spec.ManagedResources.CephCluster.UpgradeOSDRequiresHealthyPGs = true
	sc.Spec.ManagedResources.CephCluster.WaitTimeoutForHealthyOSDInMinutes = 20 * time.Minute
	sc.Spec.ManagedResources.CephCluster.OsdMaintenanceTimeout = 45 * time.Minute

	expected, err := newCephCluster(sc, "", nil, log)
	assert.NilError(t, err)
	assert.Equal(t, true, expected.Spec.ContinueUpgradeAfterChecksEvenIfNotHealthy)
	assert.Equal(t, true, expected.Spec.SkipUpgradeChecks)
	assert.Equal(t, true, expected.Spec.UpgradeOSDRequiresHealthyPGs)
	assert.Equal(t, 20*time.Minute, expected.Spec.WaitTimeoutForHealthyOSDInMinutes)
	assert.Equal(t, 45*time.Minute, expected.Spec.DisruptionManagement.OSDMaintenanceTimeout)
}
