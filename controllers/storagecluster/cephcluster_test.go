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
	tassert "github.com/stretchr/testify/assert"
	"gotest.tools/v3/assert"
	is "gotest.tools/v3/assert/cmp"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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

		expected := newCephCluster(reconciler, mockStorageCluster.DeepCopy(), nil)
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
		_, err := obj.ensureCreated(reconciler, sc)
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
				Name:      defaults.KMSConfigMapName,
				Namespace: sc.Namespace,
			},
			Data: map[string]string{
				"KMS_PROVIDER": "vault",
				"VAULT_ADDR":   "http://vault.example.com:9000",
			},
		}

		reconciler := createFakeStorageClusterReconciler(t, KMSConfigMap)

		var obj ocsCephCluster
		_, err := obj.ensureCreated(reconciler, sc)
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
		_, err := obj.ensureCreated(reconciler, sc)
		assert.NilError(t, err)

		cc := newCephCluster(reconciler, sc, nil)
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

		reconciler := createFakeStorageClusterReconciler(t)
		actual := newCephCluster(reconciler, c.sc, nil)
		assert.Equal(t, ocsutil.GenerateNameForCephCluster(c.sc), actual.Name)
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
					{Name: "balancer", Enabled: true},
					{Name: "rook", Enabled: false},
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
					{Name: "balancer", Enabled: true},
					{Name: "rook", Enabled: false},
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
					{Name: "balancer", Enabled: true},
					{Name: "rook", Enabled: false},
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
					{Name: "balancer", Enabled: true},
					{Name: "rook", Enabled: false},
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
	cases := []struct {
		label               string
		setupStorageCluster func() *ocsv1.StorageCluster
		hasCustomPlacement  bool
	}{
		{
			label: "Zone failure domain with 3 zones",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				sc.Spec.Encryption.ClusterWide = false
				sc.Spec.StorageDeviceSets = mockDeviceSets
				sc.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
				return sc
			},
		},
		{
			label: "Zone failure domain with 3 zones, cluster-wide encryption",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				sc.Spec.Encryption.ClusterWide = true
				sc.Spec.StorageDeviceSets = mockDeviceSets
				sc.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
				sc.Spec.LabelSelector = &metav1.LabelSelector{
					MatchExpressions: []metav1.LabelSelectorRequirement{},
				}
				return sc
			},
		},
		{
			label: "Rack failure domain",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				sc.Spec.StorageDeviceSets = mockDeviceSets
				sc.Status.FailureDomainKey = defaults.RackTopologyKey
				return sc
			},
		},
		{
			label: "Host failure domain",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				sc.Spec.StorageDeviceSets = mockDeviceSets
				sc.Status.FailureDomainKey = corev1.LabelHostname
				return sc
			},
		},
		{
			label: "Device set with custom OSD placement only",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				customDeviceSet := mockDeviceSets[0]
				customDeviceSet.Placement = rookCephv1.Placement{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "custom-osd-label",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"osd-value"},
										},
									},
								},
							},
						},
					},
				}
				sc.Spec.StorageDeviceSets = []ocsv1.StorageDeviceSet{customDeviceSet}
				sc.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
				return sc
			},
			hasCustomPlacement: true,
		},
		{
			label: "Device set with both custom OSD and prepare placements",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				customDeviceSet := mockDeviceSets[0]
				customDeviceSet.Placement = rookCephv1.Placement{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "osd-node-type",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"storage"},
										},
									},
								},
							},
						},
					},
				}
				customDeviceSet.PreparePlacement = rookCephv1.Placement{
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "prepare-node-type",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"compute"},
										},
									},
								},
							},
						},
					},
				}
				sc.Spec.StorageDeviceSets = []ocsv1.StorageDeviceSet{customDeviceSet}
				sc.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
				return sc
			},
			hasCustomPlacement: true,
		},
		{
			label: "Device set with custom placement and tolerations",
			setupStorageCluster: func() *ocsv1.StorageCluster {
				sc := &ocsv1.StorageCluster{}
				customDeviceSet := mockDeviceSets[0]
				customDeviceSet.Placement = rookCephv1.Placement{
					Tolerations: []corev1.Toleration{
						{
							Key:      "storage-node",
							Operator: corev1.TolerationOpEqual,
							Value:    "dedicated",
							Effect:   corev1.TaintEffectNoSchedule,
						},
					},
					NodeAffinity: &corev1.NodeAffinity{
						RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
							NodeSelectorTerms: []corev1.NodeSelectorTerm{
								{
									MatchExpressions: []corev1.NodeSelectorRequirement{
										{
											Key:      "node-role",
											Operator: corev1.NodeSelectorOpIn,
											Values:   []string{"storage-worker"},
										},
									},
								},
							},
						},
					},
				}
				sc.Spec.StorageDeviceSets = []ocsv1.StorageDeviceSet{customDeviceSet}
				sc.Status.FailureDomainKey = defaults.RackTopologyKey
				return sc
			},
			hasCustomPlacement: true,
		},
	}

	for _, c := range cases {
		t.Run(c.label, func(t *testing.T) {
			sc := c.setupStorageCluster()
			actual := newStorageClassDeviceSets(sc)
			assert.Equal(t, defaults.DeviceSetReplica, len(actual))
			deviceSet := sc.Spec.StorageDeviceSets[0]

			for i, scds := range actual {
				assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
				assert.Equal(t, deviceSet.Count/defaults.DeviceSetReplica, scds.Count)
				assert.DeepEqual(t, getDaemonResources("osd", sc), scds.Resources)
				assert.DeepEqual(t, deviceSet.DataPVCTemplate.ObjectMeta, scds.VolumeClaimTemplates[0].ObjectMeta)
				assert.DeepEqual(t, deviceSet.DataPVCTemplate.Spec, scds.VolumeClaimTemplates[0].Spec)
				assert.Equal(t, true, scds.Portable)
				assert.Equal(t, sc.Spec.Encryption.ClusterWide, scds.Encrypted)
				defaultExpectedOSDPlacement := getPlacement(sc, "osd")
				defaultExpectedPreparePlacement := getPlacement(sc, "prepareosd")

				// create a device class match expression
				deviceClassMatchExpression := metav1.LabelSelectorRequirement{
					Key:      deviceClassPlacementKey,
					Operator: metav1.LabelSelectorOpIn,
					Values:   []string{deviceSet.DeviceClass},
				}
				// append the device class match expression to the topology spread constraints
				// we are appending to the first topology spread constraint as the first one will have topologyKey as failureDomain
				expectedOSDPlacement := *defaultExpectedOSDPlacement.DeepCopy()
				expectedOSDPlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions = append(expectedOSDPlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions, deviceClassMatchExpression)
				expectedPreparePlacement := *defaultExpectedPreparePlacement.DeepCopy()
				expectedPreparePlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions = append(expectedPreparePlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions, deviceClassMatchExpression)

				if c.hasCustomPlacement {
					// Handle different custom placement scenarios based on the new placement logic
					switch {
					case !isPlacementEmpty(deviceSet.Placement) && !isPlacementEmpty(deviceSet.PreparePlacement):
						// Both OSD and prepare placements are specified
						expectedOSDPlacement = mergePlacements(expectedOSDPlacement, deviceSet.Placement)
						expectedPreparePlacement = mergePlacements(expectedPreparePlacement, deviceSet.PreparePlacement)
					case isPlacementEmpty(deviceSet.Placement) && !isPlacementEmpty(deviceSet.PreparePlacement):
						// Only prepare placement is specified
						expectedPreparePlacement = mergePlacements(expectedPreparePlacement, deviceSet.PreparePlacement)
					case !isPlacementEmpty(deviceSet.Placement) && isPlacementEmpty(deviceSet.PreparePlacement):
						// Only OSD placement is specified - prepare placement uses the OSD placement
						expectedOSDPlacement = mergePlacements(expectedOSDPlacement, deviceSet.Placement)
						expectedPreparePlacement = expectedOSDPlacement
					}
				}

				assert.DeepEqual(t, expectedOSDPlacement, scds.Placement)
				assert.DeepEqual(t, &expectedPreparePlacement, scds.PreparePlacement)
			}
		})
	}
}

func createDummyKMSConfigMap(kmsProvider, kmsAddr string, kmsAuthMethod string) *corev1.ConfigMap {
	cm := &corev1.ConfigMap{}
	cm.Name = defaults.KMSConfigMapName
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
	_, err := obj.ensureCreated(reconciler, cr)
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
		types.NamespacedName{Name: ocsutil.GenerateNameForCephCluster(cr)},
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
			corev1.LabelZoneFailureDomainStable: []string{
				"zone1",
				"zone2",
			},
		},
		ArbiterLocation: "zone3",
	}
	sc1.Status.FailureDomain = "zone"
	sc1.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
	sc1.Status.FailureDomainValues = []string{"zone1", "zone2"}

	sc2 := &ocsv1.StorageCluster{}
	sc2.Spec.Arbiter.Enable = true
	sc2.Spec.NodeTopologies = &ocsv1.NodeTopologyMap{
		ArbiterLocation: "zone3",
	}
	sc2.Spec.StorageDeviceSets = getMockDeviceSets("mock", 1, 6, true)
	sc2.Status.NodeTopologies = &ocsv1.NodeTopologyMap{
		Labels:          map[string]ocsv1.TopologyLabelValues{corev1.LabelZoneFailureDomainStable: []string{"zone1", "zone2"}},
		ArbiterLocation: "zone3",
	}
	sc2.Status.FailureDomain = "zone"
	sc2.Status.FailureDomainKey = corev1.LabelZoneFailureDomainStable
	sc2.Status.FailureDomainValues = []string{"zone1", "zone2"}

	cases := []struct {
		label       string
		sc          *ocsv1.StorageCluster
		topologyKey string
	}{
		{
			label:       "case 1",
			sc:          sc1,
			topologyKey: corev1.LabelZoneFailureDomainStable,
		},
		{
			label:       "case 2",
			sc:          sc2,
			topologyKey: corev1.LabelZoneFailureDomainStable,
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)

		setFailureDomain(c.sc)
		actual := newStorageClassDeviceSets(c.sc)
		deviceSet := c.sc.Spec.StorageDeviceSets[0]

		for i, scds := range actual {
			defaultOsdPlacement := getPlacement(c.sc, "osd")
			// create a device class match expression
			deviceClassMatchExpression := metav1.LabelSelectorRequirement{
				Key:      deviceClassPlacementKey,
				Operator: metav1.LabelSelectorOpIn,
				Values:   []string{deviceSet.DeviceClass},
			}
			osdPlacement := *defaultOsdPlacement.DeepCopy()
			osdPlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions = append(osdPlacement.TopologySpreadConstraints[0].LabelSelector.MatchExpressions, deviceClassMatchExpression)

			assert.Equal(t, fmt.Sprintf("%s-%d", deviceSet.Name, i), scds.Name)
			assert.Equal(t, deviceSet.Count, scds.Count)
			assert.DeepEqual(t, getDaemonResources("osd", c.sc), scds.Resources)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.ObjectMeta, scds.VolumeClaimTemplates[0].ObjectMeta)
			assert.DeepEqual(t, deviceSet.DataPVCTemplate.Spec, scds.VolumeClaimTemplates[0].Spec)
			assert.Equal(t, true, scds.Portable)
			assert.Equal(t, c.sc.Spec.Encryption.ClusterWide, scds.Encrypted)
			assert.DeepEqual(t, osdPlacement, scds.Placement)
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
				"mgr":            defaults.BalancedDaemonResources["mgr"],
				"mon":            defaults.BalancedDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"mgr":            defaults.LeanDaemonResources["mgr"],
				"mon":            defaults.LeanDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"mgr":            defaults.BalancedDaemonResources["mgr"],
				"mon":            defaults.BalancedDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"mgr":            defaults.PerformanceDaemonResources["mgr"],
				"mon":            defaults.PerformanceDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"mon":            defaults.PerformanceDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
				"logcollector": defaults.DaemonResources["logcollector"],
				"exporter":     defaults.DaemonResources["exporter"],
				"mgr-sidecar":  defaults.DaemonResources["mgr-sidecar"],
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
				"logcollector": defaults.DaemonResources["logcollector"],
				"exporter":     defaults.DaemonResources["exporter"],
				"mgr-sidecar":  defaults.DaemonResources["mgr-sidecar"],
			},
		},
		{
			name: "All resource keys are included when resources are non-empty",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mgr-sidecar": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("100m"),
								corev1.ResourceMemory: resource.MustParse("128Mi"),
							},
						},
						"cmd-reporter": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("50m"),
								corev1.ResourceMemory: resource.MustParse("64Mi"),
							},
						},
						"cleanup": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("25m"),
								corev1.ResourceMemory: resource.MustParse("32Mi"),
							},
						},
					},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr":            defaults.BalancedDaemonResources["mgr"],
				"mon":            defaults.BalancedDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
				"cmd-reporter": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("50m"),
						corev1.ResourceMemory: resource.MustParse("64Mi"),
					},
				},
				"cleanup": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("25m"),
						corev1.ResourceMemory: resource.MustParse("32Mi"),
					},
				},
			},
		},
		{
			name: "Empty resource requirements are filtered out",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mgr": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU:    resource.MustParse("1"),
								corev1.ResourceMemory: resource.MustParse("2Gi"),
							},
						},
						"empty-resource": {}, // This should be filtered out
					},
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("1"),
						corev1.ResourceMemory: resource.MustParse("2Gi"),
					},
				},
				"mon":            defaults.BalancedDaemonResources["mon"],
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
			},
		},
		{
			name: "Resource merging with partial specifications",
			sc: &ocsv1.StorageCluster{
				Spec: ocsv1.StorageClusterSpec{
					Resources: map[string]corev1.ResourceRequirements{
						"mgr": {
							Requests: corev1.ResourceList{
								corev1.ResourceCPU: resource.MustParse("500m"), // Only CPU request specified
							},
						},
						"mon": {
							Limits: corev1.ResourceList{
								corev1.ResourceMemory: resource.MustParse("4Gi"), // Only memory limit specified
							},
						},
					},
					ResourceProfile: "lean",
				},
			},
			expected: map[string]corev1.ResourceRequirements{
				"mgr": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("500m"),                                          // Specified CPU request
						corev1.ResourceMemory: defaults.LeanDaemonResources["mgr"].Requests[corev1.ResourceMemory], // Default memory request
					},
					Limits: corev1.ResourceList{
						corev1.ResourceMemory: defaults.LeanDaemonResources["mgr"].Limits[corev1.ResourceMemory], // Default memory limit (CPU limit not included since CPU request was specified)
					},
				},
				"mon": {
					Requests: corev1.ResourceList{
						corev1.ResourceCPU: defaults.LeanDaemonResources["mon"].Requests[corev1.ResourceCPU], // Default CPU request (memory request not included since memory limit was specified)
					},
					Limits: corev1.ResourceList{
						corev1.ResourceCPU:    defaults.LeanDaemonResources["mon"].Limits[corev1.ResourceCPU], // Default CPU limit
						corev1.ResourceMemory: resource.MustParse("4Gi"),                                      // Specified memory limit
					},
				},
				"crashcollector": defaults.DaemonResources["crashcollector"],
				"logcollector":   defaults.DaemonResources["logcollector"],
				"exporter":       defaults.DaemonResources["exporter"],
				"mgr-sidecar":    defaults.DaemonResources["mgr-sidecar"],
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
	assert.Equal(t, 12, len(prometheusRules.Spec.Groups))

	prometheusRules, err = parsePrometheusRule(externalPrometheusRules)
	assert.NilError(t, err)
	assert.Equal(t, 1, len(prometheusRules.Spec.Groups))
}

func TestChangePrometheusExprFunc(t *testing.T) {
	prometheusRule, err := parsePrometheusRule(localPrometheusRules)
	assert.NilError(t, err)
	var changeTokens = []replaceToken{
		{recordOrAlertName: "CephMgrIsAbsent", wordToReplace: "openshift-storage", replaceWith: "new-namespace"},
		// when alert or record name is not specified,
		// the change should affect all the expressions which has the 'wordInExpr'
		{recordOrAlertName: "", wordToReplace: "ceph_pool_stored", replaceWith: "new_ceph_pool_stored"},
		{recordOrAlertName: "", wordToReplace: "0.75", replaceWith: "0.775"},
		{recordOrAlertName: "", wordToReplace: "85%", replaceWith: "92.50%"},
	}
	changePromRule(prometheusRule, changeTokens)

	recordOrAlertNameAndReplacedWord := [][2]string{
		{"CephMgrIsAbsent", "new-namespace"},
		{"CephPoolQuotaBytesNearExhaustion", "new_ceph_pool_stored"},
		{"CephPoolQuotaBytesCriticallyExhausted", "new_ceph_pool_stored"},
		{"CephClusterNearFull", "0.775"},
		{"CephOSDNearFull", "0.775"},
		{"CephClusterNearFull", "92.50%"},
		{"CephClusterCriticallyFull", "92.50%"},
		{"CephClusterReadOnly", "92.50%"},
	}
	for _, grp := range prometheusRule.Spec.Groups {
		for _, rule := range grp.Rules {
			for _, eachChange := range recordOrAlertNameAndReplacedWord {
				alertName := eachChange[0]
				changeStr := eachChange[1]
				if rule.Alert != alertName {
					continue
				}
				assert.Assert(t,
					strings.Contains(rule.Expr.String(), changeStr) ||
						(rule.Annotations != nil && strings.Contains(rule.Annotations["description"], changeStr)),
					fmt.Sprintf("Expected '%s' to be found in either Expr or Annotations for alert %s", changeStr, alertName))
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
	maxLogSize := resource.MustParse("500Mi")
	reconciler := createFakeStorageClusterReconciler(t)

	defaultLogCollector := rookCephv1.LogCollectorSpec{
		Enabled:     true,
		Periodicity: "daily",
		MaxLogSize:  &maxLogSize,
	}

	sc.Spec.LogCollector = &defaultLogCollector

	actual := newCephCluster(reconciler, sc, nil)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	// when disabled in storageCluster
	sc.Spec.LogCollector = &rookCephv1.LogCollectorSpec{}
	actual = newCephCluster(reconciler, sc, nil)
	assert.DeepEqual(t, actual.Spec.LogCollector, defaultLogCollector)

	maxLogSize = resource.MustParse("6Gi")
	sc.Spec.LogCollector.MaxLogSize = &maxLogSize

	actual = newCephCluster(reconciler, sc, nil)
	assert.DeepEqual(t, actual.Spec.LogCollector.MaxLogSize, &maxLogSize)
}

func TestCephClusterNetworkConnectionsSpec(t *testing.T) {
	reconciler := createFakeStorageClusterReconciler(t)
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
					Connections: &rookCephv1.ConnectionsSpec{
						RequireMsgr2: true,
					},
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
						RequireMsgr2: true,
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
						RequireMsgr2: true,
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
						RequireMsgr2: true,
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
						RequireMsgr2: true,
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
		cc := newCephCluster(reconciler, sc, nil)
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

func TestEnsureRDRMigration(t *testing.T) {
	testSkipPrometheusRules = true
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
	reconciler := createFakeStorageClusterReconciler(t, networkConfig)

	expected := newCephCluster(reconciler, mockStorageCluster.DeepCopy(), nil)

	expected.Spec.Storage.Store.Type = string(rookCephv1.StoreTypeBlueStoreRDR)
	err := reconciler.Client.Create(context.TODO(), expected)
	assert.NilError(t, err)

	// Ensure bluestore-rdr store type is reset to bluestore
	var obj ocsCephCluster
	_, err = obj.ensureCreated(reconciler, sc)
	assert.NilError(t, err)
	actual := &rookCephv1.CephCluster{}
	err = reconciler.Client.Get(context.TODO(), types.NamespacedName{Name: ocsutil.GenerateNameForCephClusterFromString(sc.Name), Namespace: sc.Namespace}, actual)
	assert.NilError(t, err)
	assert.Equal(t, string(rookCephv1.StoreTypeBlueStore), actual.Spec.Storage.Store.Type)
	assert.Equal(t, "yes-really-update-store", actual.Spec.Storage.Store.UpdateStore)
}

func TestEnsureUpgradeReliabilityParams(t *testing.T) {
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	reconciler := createFakeStorageClusterReconciler(t)
	sc.Status.Images.Ceph = &ocsv1.ComponentImageStatus{}
	defaultContinueUpgradeAfterChecksEvenIfNotHealthyVal := true
	sc.Spec.ManagedResources.CephCluster.ContinueUpgradeAfterChecksEvenIfNotHealthy = &defaultContinueUpgradeAfterChecksEvenIfNotHealthyVal
	sc.Spec.ManagedResources.CephCluster.SkipUpgradeChecks = true
	sc.Spec.ManagedResources.CephCluster.UpgradeOSDRequiresHealthyPGs = true
	sc.Spec.ManagedResources.CephCluster.WaitTimeoutForHealthyOSDInMinutes = 20 * time.Minute
	sc.Spec.ManagedResources.CephCluster.OsdMaintenanceTimeout = 45 * time.Minute

	expected := newCephCluster(reconciler, sc, nil)
	assert.Equal(t, true, expected.Spec.ContinueUpgradeAfterChecksEvenIfNotHealthy)
	assert.Equal(t, true, expected.Spec.SkipUpgradeChecks)
	assert.Equal(t, true, expected.Spec.UpgradeOSDRequiresHealthyPGs)
	assert.Equal(t, 20*time.Minute, expected.Spec.WaitTimeoutForHealthyOSDInMinutes)
	assert.Equal(t, 45*time.Minute, expected.Spec.DisruptionManagement.OSDMaintenanceTimeout)
}

func TestHealthCheckConfiguration(t *testing.T) {
	sc := &ocsv1.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)
	reconciler := createFakeStorageClusterReconciler(t)
	interval := metav1.Duration{
		Duration: 20 * time.Second,
	}
	mockProbeSpec := &rookCephv1.ProbeSpec{
		Disabled: false,
		Probe: &corev1.Probe{
			InitialDelaySeconds: 10,
			TimeoutSeconds:      5,
		},
	}
	probeMap := make(map[rookCephv1.KeyType]*rookCephv1.ProbeSpec)
	probeMap["abc"] = mockProbeSpec

	sc.Spec.ManagedResources.CephCluster.HealthCheck = &rookCephv1.CephClusterHealthCheckSpec{
		DaemonHealth: rookCephv1.DaemonHealthSpec{
			Status: rookCephv1.HealthCheckSpec{
				Timeout:  "11",
				Disabled: false,
				Interval: &interval,
			},
			Monitor: rookCephv1.HealthCheckSpec{
				Timeout:  "22",
				Disabled: true,
				Interval: &interval,
			},
			ObjectStorageDaemon: rookCephv1.HealthCheckSpec{
				Timeout:  "33",
				Disabled: false,
				Interval: &interval,
			},
		},
		StartupProbe:  probeMap,
		LivenessProbe: probeMap,
	}
	expected := newCephCluster(reconciler, sc, nil)

	assert.Equal(t, "11", expected.Spec.HealthCheck.DaemonHealth.Status.Timeout)
	assert.Equal(t, false, expected.Spec.HealthCheck.DaemonHealth.Status.Disabled)
	assert.Equal(t, &interval, expected.Spec.HealthCheck.DaemonHealth.Status.Interval)

	assert.Equal(t, "22", expected.Spec.HealthCheck.DaemonHealth.Monitor.Timeout)
	assert.Equal(t, true, expected.Spec.HealthCheck.DaemonHealth.Monitor.Disabled)
	assert.Equal(t, &interval, expected.Spec.HealthCheck.DaemonHealth.Monitor.Interval)

	assert.Equal(t, "33", expected.Spec.HealthCheck.DaemonHealth.ObjectStorageDaemon.Timeout)
	assert.Equal(t, false, expected.Spec.HealthCheck.DaemonHealth.ObjectStorageDaemon.Disabled)
	assert.Equal(t, &interval, expected.Spec.HealthCheck.DaemonHealth.ObjectStorageDaemon.Interval)

	compareProbeMaps(t, probeMap, expected.Spec.HealthCheck.LivenessProbe)
	compareProbeMaps(t, probeMap, expected.Spec.HealthCheck.StartupProbe)

}

// Helper function to compare two maps
func compareProbeMaps(t *testing.T, map1, map2 map[rookCephv1.KeyType]*rookCephv1.ProbeSpec) {
	assert.Equal(t, len(map1), len(map2))

	for key, value1 := range map1 {
		value2, exists := map2[key]
		assert.Assert(t, exists, "Key %v not found in map2", key)

		// Compare the actual ProbeSpec values
		assert.Equal(t, value1.Disabled, value2.Disabled)
		assert.Equal(t, value1.Probe.InitialDelaySeconds, value2.Probe.InitialDelaySeconds)
		assert.Equal(t, value1.Probe.TimeoutSeconds, value2.Probe.TimeoutSeconds)
	}
}

func TestDetermineDefaultCephDeviceClass(t *testing.T) {
	cases := []struct {
		label                 string
		foundDeviceClasses    []rookCephv1.DeviceClasses
		isReplica1            bool
		replica1DeviceClasses []string
		expectedDeviceClass   string
	}{
		{
			label:                 "Case 1: Replica 1 not enabled & no DeviceClass in status",
			foundDeviceClasses:    []rookCephv1.DeviceClasses{},
			isReplica1:            false,
			replica1DeviceClasses: []string{},
			expectedDeviceClass:   "",
		},
		{
			label: "Case 2: Replica 1 not enabled & 1 DeviceClass is in status",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
			},
			isReplica1:            false,
			replica1DeviceClasses: []string{},
			expectedDeviceClass:   "gold",
		},
		{
			label: "Case 3: Replica 1 not enabled & more than 1 DeviceClass is in status",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
				{
					Name: "silver",
				},
			},
			isReplica1:            false,
			replica1DeviceClasses: []string{},
			expectedDeviceClass:   "gold",
		},
		{
			label:                 "Case 4: Replica 1 enabled & no DeviceClass in status",
			foundDeviceClasses:    []rookCephv1.DeviceClasses{},
			isReplica1:            true,
			replica1DeviceClasses: []string{"zone1", "zone2", "zone3"},
			expectedDeviceClass:   "",
		},
		{
			label: "Case 5:  Replica 1 enabled. Total less than n DeviceClass are in status, with only one non replica-1 DeviceClass",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
			},
			isReplica1:            true,
			replica1DeviceClasses: []string{"zone1", "zone2", "zone3"},
			expectedDeviceClass:   "gold",
		},
		{
			label: "Case 6:  Replica 1 enabled. Total less than n DeviceClass are in status, with more than one non replica-1 DeviceClass",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
				{
					Name: "silver",
				},
			},
			isReplica1:          true,
			expectedDeviceClass: "gold",
		},
		{
			label: "Case 7: Replica 1 enabled & only n replica-1 DeviceClass are in status without any non-replica-1 DeviceClass",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "zone1",
				},
				{
					Name: "zone2",
				},
				{
					Name: "zone3",
				},
			},
			isReplica1:            true,
			replica1DeviceClasses: []string{"zone1", "zone2", "zone3"},
			expectedDeviceClass:   "",
		},
		{
			label: "Case 8: Replica 1 enabled & n+1 total DeviceClass are in status(n replica-1 DeviceClass, 1 non-replica-1 DeviceClass)",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
				{
					Name: "zone1",
				},
				{
					Name: "zone2",
				},
				{
					Name: "zone3",
				},
			},
			isReplica1:          true,
			expectedDeviceClass: "gold",
		},
		{
			label: "Case 9: Replica 1 enabled & more than n+1 DeviceClass are in status",
			foundDeviceClasses: []rookCephv1.DeviceClasses{
				{
					Name: "gold",
				},
				{
					Name: "silver",
				},
				{
					Name: "zone1",
				},
				{
					Name: "zone2",
				},
				{
					Name: "zone3",
				},
			},
			expectedDeviceClass: "gold",
		},
	}

	for _, c := range cases {
		t.Logf("Case: %s\n", c.label)
		actual := determineDefaultCephDeviceClass(c.foundDeviceClasses, c.isReplica1, c.replica1DeviceClasses)
		assert.Equal(t, c.expectedDeviceClass, actual)
	}
}

func TestIsEncrptionSettingUpdated(t *testing.T) {
	newStorageClassDeviceSet := []rookCephv1.StorageClassDeviceSet{
		{
			Name:      "set1",
			Encrypted: false,
		},
		{
			Name:      "set2",
			Encrypted: false,
		},
	}

	// encryption setting has changed
	actualResult := isEncrptionSettingUpdated(true, newStorageClassDeviceSet)
	assert.Equal(t, true, actualResult)

	// encryption setting has not changed
	actualResult = isEncrptionSettingUpdated(false, newStorageClassDeviceSet)
	assert.Equal(t, false, actualResult)
}

func TestGetCephClusterCephConfig(t *testing.T) {
	var scName = "test-storagecluster"
	var cases = []struct {
		description    string
		storageCluster *ocsv1.StorageCluster
		addObjs        []runtime.Object
		expectedConfig map[string]map[string]string
	}{
		{
			description: "case 1: No cephConfig specified in CR - should use default values",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "200",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 2: TargetPGPerOsd & MaxPGPerOSD are specified on CR - should respect CR setting",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							CephConfig: map[string]map[string]string{
								"global": {
									"mon_target_pg_per_osd": "500",
									"mon_max_pg_per_osd":    "1500",
								},
							},
						},
					},
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1500",
					"mon_target_pg_per_osd":              "500",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 3: Other CephConfigs are specified on CR - should merge default & CR specified values",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephCluster: ocsv1.ManageCephCluster{
							CephConfig: map[string]map[string]string{
								"osd": {
									"osd_max_backfills": "4",
								},
							},
						},
					},
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "200",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
					"osd_max_backfills":                    "4",
				},
			},
		},
		{
			description: "case 4: ResourceProfile 'lean' should set mon_target_pg_per_osd to 100",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					ResourceProfile: "lean",
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "100",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 5: ResourceProfile 'performance' should set mon_target_pg_per_osd to 400",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					ResourceProfile: "performance",
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "400",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 6: CephNonResilientPools enabled should set mon_warn_on_pool_no_redundancy",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					ManagedResources: ocsv1.ManagedResourcesSpec{
						CephNonResilientPools: ocsv1.ManageCephNonResilientPools{
							Enable: true,
						},
					},
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "200",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"mon_warn_on_pool_no_redundancy":     "false",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 7: Network dualstack (not multus) should set public_network",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name: scName,
				},
				Spec: ocsv1.StorageClusterSpec{
					Network: &rookCephv1.NetworkSpec{
						DualStack: true,
						Provider:  "",
					},
				},
			},
			addObjs: []runtime.Object{
				&configv1.Network{
					ObjectMeta: metav1.ObjectMeta{Name: "cluster"},
					Status: configv1.NetworkStatus{
						ClusterNetwork: []configv1.ClusterNetworkEntry{{CIDR: "10.128.0.0/14"}},
					},
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "200",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"public_network":                     "10.128.0.0/14",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
			},
		},
		{
			description: "case 8: RBD Mirror CR present should add debug configs",
			storageCluster: &ocsv1.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: "test-ns",
					Name:      scName},
			},
			addObjs: []runtime.Object{
				&rookCephv1.CephRBDMirror{
					ObjectMeta: metav1.ObjectMeta{
						Name:      "rbd-mirror-a",
						Namespace: "test-ns",
					},
				},
			},
			expectedConfig: map[string]map[string]string{
				"global": {
					"mon_max_pg_per_osd":                 "1000",
					"mon_target_pg_per_osd":              "200",
					"mon_pg_warn_max_object_skew":        "0",
					"mon_data_avail_warn":                "15",
					"bdev_flock_retry":                   "20",
					"bluestore_prefer_deferred_size_hdd": "0",
					"bluestore_slow_ops_warn_lifetime":   "0",
					"rbd_default_pool":                   ocsutil.GenerateNameForCephBlockPool(scName),
				},
				"osd": {
					"osd_memory_target_cgroup_limit_ratio": "0.8",
				},
				"client.rbd-mirror.a": {
					"debug_ms":         "1",
					"debug_rbd":        "15",
					"debug_rbd_mirror": "30",
					"log_file":         "/var/log/ceph/$cluster-$name.log",
				},
				"client.rbd-mirror-peer": {
					"debug_ms":         "1",
					"debug_rbd":        "15",
					"debug_rbd_mirror": "30",
					"log_file":         "/var/log/ceph/$cluster-$name.log",
				},
			},
		},
	}

	for _, c := range cases {
		t.Logf("Running %s", c.description)
		reconciler := createFakeStorageClusterReconciler(t, c.addObjs...)
		actual := getCephClusterCephConfig(reconciler, c.storageCluster)
		tassert.Equal(t, c.expectedConfig, actual)
	}
}

func TestSetDefaultDataPoolSpec(t *testing.T) {
	baseSC := mockStorageCluster.DeepCopy()
	baseSC.Status.DefaultCephDeviceClass = "ssd"
	baseSC.Status.FailureDomain = "host"

	cases := []struct {
		name    string
		pool    rookCephv1.PoolSpec
		sc      *ocsv1.StorageCluster
		expects rookCephv1.PoolSpec
	}{
		{
			name: "all fields unset",
			pool: rookCephv1.PoolSpec{},
			sc:   baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "data"),
			},
		},
		{
			name: "EnableCrushUpdates set to false",
			pool: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "data"),
			},
		},
		{
			name: "only deviceClass set",
			pool: rookCephv1.PoolSpec{
				DeviceClass: "gold",
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "gold",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "data"),
			},
		},
		{
			name: "only Replicated targetSizeRatio set",
			pool: rookCephv1.PoolSpec{
				Replicated: rookCephv1.ReplicatedSpec{
					TargetSizeRatio: 0.1,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "data").Size,
					ReplicasPerFailureDomain: generateCephReplicatedSpec(baseSC, "data").ReplicasPerFailureDomain,
					TargetSizeRatio:          0.1,
				},
			},
		},
		{
			name: "EnableCrushUpdates, DeviceClass & Replicated targetSizeRatio set",
			pool: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				DeviceClass:        "nvme",
				Replicated: rookCephv1.ReplicatedSpec{
					TargetSizeRatio: 0.2,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				DeviceClass:        "nvme",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "data").Size,
					ReplicasPerFailureDomain: generateCephReplicatedSpec(baseSC, "data").ReplicasPerFailureDomain,
					TargetSizeRatio:          0.2,
				},
			},
		},
		{
			name: "arbiter mode- Replicated size & replicasPerFailureDomain should be set to default values",
			pool: rookCephv1.PoolSpec{
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     3,
					ReplicasPerFailureDomain: 1,
					TargetSizeRatio:          0.3,
				},
			},
			sc: func() *ocsv1.StorageCluster {
				sc := baseSC.DeepCopy()
				sc.Spec.Arbiter.Enable = true
				return sc
			}(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "data").Size,
					ReplicasPerFailureDomain: generateCephReplicatedSpec(baseSC, "data").ReplicasPerFailureDomain,
					TargetSizeRatio:          0.3,
				},
			},
		},
		{
			name: "erasure-coded spec present- ReplicatedSpec should not be set",
			pool: rookCephv1.PoolSpec{
				ErasureCoded: rookCephv1.ErasureCodedSpec{
					DataChunks:   4,
					CodingChunks: 2,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				ErasureCoded: rookCephv1.ErasureCodedSpec{
					DataChunks:   4,
					CodingChunks: 2,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := c.pool
			// Patch expected Replicated values for arbiter mode at runtime
			if c.name == "arbiter mode- Replicated size & replicasPerFailureDomain should be set to default values" {
				c.expects.Replicated.Size = generateCephReplicatedSpec(c.sc, "data").Size
				c.expects.Replicated.ReplicasPerFailureDomain = generateCephReplicatedSpec(c.sc, "data").ReplicasPerFailureDomain
			}
			setDefaultDataPoolSpec(&pool, c.sc)
			// Only compare relevant fields
			assert.DeepEqual(t, pool.EnableCrushUpdates, c.expects.EnableCrushUpdates)
			assert.Equal(t, pool.DeviceClass, c.expects.DeviceClass)
			assert.Equal(t, pool.FailureDomain, c.expects.FailureDomain)
			assert.DeepEqual(t, pool.Replicated, c.expects.Replicated)
			assert.DeepEqual(t, pool.ErasureCoded, c.expects.ErasureCoded)
		})
	}
}

func TestSetDefaultMetadataPoolSpec(t *testing.T) {
	baseSC := mockStorageCluster.DeepCopy()
	baseSC.Status.DefaultCephDeviceClass = "ssd"
	baseSC.Status.FailureDomain = "host"

	cases := []struct {
		name    string
		pool    rookCephv1.PoolSpec
		sc      *ocsv1.StorageCluster
		expects rookCephv1.PoolSpec
	}{
		{
			name: "all fields unset",
			pool: rookCephv1.PoolSpec{},
			sc:   baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "metadata"),
			},
		},
		{
			name: "only deviceClass set",
			pool: rookCephv1.PoolSpec{
				DeviceClass: "gold",
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "gold",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "metadata"),
			},
		},
		{
			name: "only ReplicaPerFailureDomain set",
			pool: rookCephv1.PoolSpec{
				Replicated: rookCephv1.ReplicatedSpec{
					ReplicasPerFailureDomain: 2,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "metadata").Size,
					ReplicasPerFailureDomain: 2,
				},
			},
		},
		{
			name: "arbiter mode- Replicated size & replicasPerFailureDomain should be set to default values",
			pool: rookCephv1.PoolSpec{
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     3,
					ReplicasPerFailureDomain: 1,
				},
			},
			sc: func() *ocsv1.StorageCluster {
				sc := baseSC.DeepCopy()
				sc.Spec.Arbiter.Enable = true
				return sc
			}(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "metadata").Size,
					ReplicasPerFailureDomain: generateCephReplicatedSpec(baseSC, "metadata").ReplicasPerFailureDomain,
				},
			},
		},
		{
			name: "erasure-coded spec present- ReplicatedSpec should not be set",
			pool: rookCephv1.PoolSpec{
				ErasureCoded: rookCephv1.ErasureCodedSpec{
					DataChunks:   4,
					CodingChunks: 2,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(true),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				ErasureCoded: rookCephv1.ErasureCodedSpec{
					DataChunks:   4,
					CodingChunks: 2,
				},
			},
		},
		{
			name: "EnableCrushUpdates set to false should be preserved",
			pool: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated:         generateCephReplicatedSpec(baseSC, "metadata"),
			},
		},
		{
			name: "EnableCrushUpdates false with ReplicasPerFailureDomain set",
			pool: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				Replicated: rookCephv1.ReplicatedSpec{
					ReplicasPerFailureDomain: 3,
				},
			},
			sc: baseSC.DeepCopy(),
			expects: rookCephv1.PoolSpec{
				EnableCrushUpdates: ptr.To(false),
				DeviceClass:        "ssd",
				FailureDomain:      "host",
				Replicated: rookCephv1.ReplicatedSpec{
					Size:                     generateCephReplicatedSpec(baseSC, "metadata").Size,
					ReplicasPerFailureDomain: 3,
				},
			},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pool := c.pool
			// Patch expected Replicated values for arbiter mode at runtime
			if c.name == "arbiter mode- Replicated size & replicasPerFailureDomain should be set to default values" {
				c.expects.Replicated.Size = generateCephReplicatedSpec(c.sc, "metadata").Size
				c.expects.Replicated.ReplicasPerFailureDomain = generateCephReplicatedSpec(c.sc, "metadata").ReplicasPerFailureDomain
			}
			setDefaultMetadataPoolSpec(&pool, c.sc)
			// Only compare relevant fields
			assert.DeepEqual(t, pool.EnableCrushUpdates, c.expects.EnableCrushUpdates)
			assert.Equal(t, pool.DeviceClass, c.expects.DeviceClass)
			assert.Equal(t, pool.FailureDomain, c.expects.FailureDomain)
			assert.DeepEqual(t, pool.Replicated, c.expects.Replicated)
			assert.DeepEqual(t, pool.ErasureCoded, c.expects.ErasureCoded)
		})
	}
}
