package storagecluster

import (
	"context"
	"fmt"
	"net"
	"os"
	"regexp"
	"testing"
	"time"

	opverion "github.com/operator-framework/api/pkg/lib/version"
	opv1a1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1a1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	ocsversion "github.com/red-hat-storage/ocs-operator/v4/version"

	"github.com/blang/semver/v4"
	csiaddonsv1alpha1 "github.com/csi-addons/kubernetes-csi-addons/api/csiaddons/v1alpha1"
	groupsnapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	snapapi "github.com/kubernetes-csi/external-snapshotter/client/v8/apis/volumesnapshot/v1"
	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	routev1 "github.com/openshift/api/route/v1"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	odfgsapiv1b1 "github.com/red-hat-storage/external-snapshotter/client/v8/apis/volumegroupsnapshot/v1beta1"
	ocsclientv1a1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/defaults"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/platform"
	statusutil "github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	storagev1 "k8s.io/api/storage/v1"
	extv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const (
	zoneTopologyLabel   = corev1.LabelTopologyZone
	regionTopologyLabel = corev1.LabelTopologyRegion
	hostnameLabel       = corev1.LabelHostname
	cephFSID            = "b88c2d78-9de9-4227-9313-a63f62f78743"
)

var mockStorageClusterRequest = reconcile.Request{
	NamespacedName: types.NamespacedName{
		Name:      "storage-test",
		Namespace: "storage-test-ns",
	},
}

var mockStorageCluster = &api.StorageCluster{
	TypeMeta: metav1.TypeMeta{
		Kind: "StorageCluster",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name:      mockStorageClusterRequest.Name,
		Namespace: mockStorageClusterRequest.Namespace,
		Annotations: map[string]string{
			UninstallModeAnnotation: string(UninstallModeGraceful),
			CleanupPolicyAnnotation: string(CleanupPolicyDelete),
		},
		Finalizers: []string{storageClusterFinalizer},
	},
	Spec: api.StorageClusterSpec{
		Monitoring: &api.MonitoringSpec{
			ReconcileStrategy: string(ReconcileStrategyIgnore),
		},
		LogCollector: &rookCephv1.LogCollectorSpec{
			Enabled: false,
		},
	},
}

var mockCephCluster = &rookCephv1.CephCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      statusutil.GenerateNameForCephCluster(mockStorageCluster.DeepCopy()),
		Namespace: mockStorageCluster.Namespace,
	},
}

var mockCephClusterNamespacedName = types.NamespacedName{
	Name:      statusutil.GenerateNameForCephCluster(mockStorageCluster.DeepCopy()),
	Namespace: mockStorageCluster.Namespace,
}

var storageClassName = "gp2-csi"
var storageClassName2 = "managed-premium"
var fakeStorageClassName = "st1"
var volMode = corev1.PersistentVolumeBlock
var annotations = map[string]string{
	"crushDeviceClass": "",
}

var fakeStorageClass = &storagev1.StorageClass{
	ObjectMeta: metav1.ObjectMeta{
		Name: fakeStorageClassName,
	},
	Provisioner: string(EBS),
	Parameters: map[string]string{
		"type": "fake",
	},
}

var mockDataPVCTemplate = corev1.PersistentVolumeClaim{
	ObjectMeta: metav1.ObjectMeta{
		Annotations: annotations,
	},
	Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Ti"),
			},
		},
		StorageClassName: &storageClassName,
		VolumeMode:       &volMode,
	},
}

var mockMetaDataPVCTemplate = &corev1.PersistentVolumeClaim{
	Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Ti"),
			},
		},
		StorageClassName: &storageClassName,
		VolumeMode:       &volMode,
	},
}

var mockWalPVCTemplate = &corev1.PersistentVolumeClaim{
	Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.VolumeResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Ti"),
			},
		},
		StorageClassName: &storageClassName,
		VolumeMode:       &volMode,
	},
}

func getMockDeviceSets(name string, count int, replica int, portable bool) []api.StorageDeviceSet {

	return []api.StorageDeviceSet{
		{
			Name:                name,
			Count:               count,
			Replica:             replica,
			DataPVCTemplate:     *mockDataPVCTemplate.DeepCopy(),
			MetadataPVCTemplate: mockMetaDataPVCTemplate.DeepCopy(),
			WalPVCTemplate:      mockWalPVCTemplate.DeepCopy(),
			Portable:            portable,
		},
	}

}

var mockDeviceSets = []api.StorageDeviceSet{
	{
		Name:                "mock-sds",
		Count:               3,
		DataPVCTemplate:     *mockDataPVCTemplate.DeepCopy(),
		MetadataPVCTemplate: mockMetaDataPVCTemplate.DeepCopy(),
		WalPVCTemplate:      mockWalPVCTemplate.DeepCopy(),
		Portable:            true,
	},
}

var mockNodeList = &corev1.NodeList{
	TypeMeta: metav1.TypeMeta{
		Kind: "NodeList",
	},
	Items: []corev1.Node{
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node1",
				Labels: map[string]string{
					hostnameLabel:                    "node1",
					zoneTopologyLabel:                "zone1",
					defaults.NodeAffinityKey:         "",
					"node-role.kubernetes.io/worker": "",
				},
			},
			Status: corev1.NodeStatus{
				Addresses: []corev1.NodeAddress{
					{
						Type:    corev1.NodeInternalIP,
						Address: "0:0:0:0",
					},
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node2",
				Labels: map[string]string{
					hostnameLabel:            "node2",
					zoneTopologyLabel:        "zone2",
					defaults.NodeAffinityKey: "",
				},
			},
		},
		{
			TypeMeta: metav1.TypeMeta{
				Kind: "Node",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: "node3",
				Labels: map[string]string{
					hostnameLabel:            "node3",
					zoneTopologyLabel:        "zone3",
					defaults.NodeAffinityKey: "",
				},
			},
		},
	},
}

var mockInfrastructure = &configv1.Infrastructure{
	TypeMeta: metav1.TypeMeta{
		Kind: "Infrastructure",
	},
	ObjectMeta: metav1.ObjectMeta{
		Name: "cluster",
	},
	Status: configv1.InfrastructureStatus{
		Platform: "",
	},
}

func TestReconcilerImplInterface(t *testing.T) {
	reconciler := StorageClusterReconciler{}
	var i interface{} = &reconciler
	_, ok := i.(reconcile.Reconciler)
	assert.True(t, ok)
}

func TestVersionCheck(t *testing.T) {
	testcases := []struct {
		label           string
		storageCluster  *api.StorageCluster
		expectedVersion string
		errorExpected   bool
	}{
		{
			label: "Case 1", // no version is provided in the storagecluster
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test",
					Namespace: "storage-test-ns",
				},
			},
			expectedVersion: ocsversion.Version,
			errorExpected:   false,
		},
		{
			label: "Case 2", // a lower version is provided in the storagecluster
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test",
					Namespace: "storage-test-ns",
				},
				Status: api.StorageClusterStatus{
					Version: getSemVer(ocsversion.Version, 1, true),
				},
			},
			expectedVersion: ocsversion.Version,
			errorExpected:   false,
		},
		{
			label: "Case 3", // a higher version is provided in the storagecluster
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test",
					Namespace: "storage-test-ns",
				},
				Status: api.StorageClusterStatus{
					Version: getSemVer(ocsversion.Version, 1, false),
				},
			},
			expectedVersion: ocsversion.Version,
			errorExpected:   true,
		},
	}

	for _, tc := range testcases {
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster)
		err := versionCheck(tc.storageCluster, reconciler.Log)
		if tc.errorExpected {
			assert.Errorf(t, err, "[%q]: failed to assert error when higher version is provided in the storagecluster spec", tc.label)
			continue
		}
		assert.NoError(t, err)
		assert.Equalf(t, tc.expectedVersion, tc.storageCluster.Status.Version, "[%q]: failed to get correct version", tc.label)
	}

}

func TestThrottleStorageDevices(t *testing.T) {
	testcases := []struct {
		label          string
		storageClass   *storagev1.StorageClass
		deviceSets     []api.StorageDeviceSet
		storageCluster *api.StorageCluster
		expectedSpeed  diskSpeed
		platform       configv1.PlatformType
	}{
		{
			label: "Case 1", // storageclass is gp2-csi or io1
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gp2-csi",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "gp2-csi",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName,
						},
					},
					Portable: true,
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedSlow,
		},
		{
			label: "Case 2", // storageclass is neither gp2-csi nor io1
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "st1",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "st1",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &fakeStorageClassName,
						},
					},
					Portable: true,
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedUnknown,
		},
		{
			label: "Case 3", // storageclass is managed-premium
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-premium",
				},
				Provisioner: string(AzureDisk),
				Parameters: map[string]string{
					"type": "managed-premium",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName2,
						},
					},
					Portable: true,
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedFast,
		},
		{
			label: "Case 4", // storageclass is managed-premium but deviceType hdd
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "managed-premium",
				},
				Provisioner: string(AzureDisk),
				Parameters: map[string]string{
					"type": "managed-premium",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName2,
						},
					},
					Portable:   true,
					DeviceType: "hdd",
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedSlow,
		},
		{
			label: "Case 5", // storageclass is gp2-csi but deviceType ssd
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gp2-csi",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "gp2-csi",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName,
						},
					},
					Portable:   true,
					DeviceType: "ssd",
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedFast,
		},
		{
			label: "Case 6", // storageclass is neither gp2-csi nor io1 but deviceType nvme
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "st1",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "st1",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &fakeStorageClassName,
						},
					},
					Portable:   true,
					DeviceType: "nvme",
				},
			},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedFast,
		},
		{
			label: "Case 7", // storageclass is neither gp2-csi nor io1 but platform is Azure
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "st1",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "st1",
				},
			},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &fakeStorageClassName,
						},
					},
					Portable: true,
				},
			},
			platform:       configv1.AzurePlatformType,
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedFast,
		},
	}

	for _, tc := range testcases {
		platform.SetFakePlatformInstanceForTesting(true, tc.platform)
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster, tc.storageClass)
		for _, ds := range tc.deviceSets {
			actualSpeed, err := reconciler.checkTuneStorageDevices(ds)
			assert.NoError(t, err)
			assert.Equalf(t, tc.expectedSpeed, actualSpeed, "[%q]: failed to get expected output", tc.label)
		}
		platform.UnsetFakePlatformInstanceForTesting()
	}
}

func TestIsActiveStorageCluster(t *testing.T) {
	testcases := []struct {
		label           string
		storageCluster1 *api.StorageCluster
		storageCluster2 *api.StorageCluster
		isActive        bool
	}{
		{
			label: "Case 1", // storageCluster1 has phase ignored. So storageCluster2 should be active
			storageCluster1: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-a",
					Namespace: "storage-test-ns",
				},
				Status: api.StorageClusterStatus{
					Phase: statusutil.PhaseIgnored,
				},
			},
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
			},
			isActive: true,
		},
		{
			// storageCluster1 and storageCluster2 have same creationTimeStamp. So storageCluster2 is not active
			// based on the alphabetic order of the object name (storage-test-a < storage-test-b)
			label: "Case 2",
			storageCluster1: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-a",
					Namespace: "storage-test-ns",
				},
			},
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
			},
			isActive: false,
		},
		{
			// storageCluster1 and storageCluster2 have same creationTimeStamp. So storageCluster2 is active
			// based on the alphabetic order of the object name. (storage-test-z > storage-test-b)
			label: "Case 3",
			storageCluster1: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-z",
					Namespace: "storage-test-ns",
				},
			},
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
			},
			isActive: true,
		},
		{
			label: "Case 4", // storageCluster2 should be active as there are no other storageClusters available
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
			},
			isActive: true,
		},
		{
			label: "Case 5", // internal storageCluster should be ignored if it is another namespace than operatorNamespace
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test",
				},
			},
			isActive: false,
		},
		{
			label: "Case 6", // external storageCluster should be allowed if it is in another namespace than operatorNamespace
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test",
				},
				Spec: api.StorageClusterSpec{
					ExternalStorage: api.ExternalStorageClusterSpec{
						Enable: true,
					},
				},
			},
			isActive: true,
		},
		{
			label: "Case 7", // external storageCluster should be allowed if it is in same namespace as operatorNamespace
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
				Spec: api.StorageClusterSpec{
					ExternalStorage: api.ExternalStorageClusterSpec{
						Enable: true,
					},
				},
			},
			isActive: true,
		},
	}

	os.Setenv("OPERATOR_NAMESPACE", "storage-test-ns")

	for _, tc := range testcases {

		var reconciler StorageClusterReconciler
		if tc.storageCluster1 != nil && tc.storageCluster2 != nil {
			reconciler = createFakeStorageClusterReconciler(t, tc.storageCluster1, tc.storageCluster2)
		} else {
			reconciler = createFakeStorageClusterReconciler(t, tc.storageCluster2)
		}

		actual := reconciler.isActiveStorageCluster(tc.storageCluster2)
		assert.Equalf(t, tc.isActive, actual, "[%q] failed to assert if current storagecluster is active or not", tc.label)
	}

}
func TestReconcileWithNonWatchedResource(t *testing.T) {
	testcases := []struct {
		label     string
		name      string
		namespace string
	}{
		{
			label:     "case 1", // resource with non-watched name
			name:      "doesn't exist",
			namespace: "storage-test-ns",
		},
		{
			label:     "case 1", // resource with non-watched namespace
			name:      "storage-test",
			namespace: "doesn't exist",
		},
	}

	for _, tc := range testcases {
		request := reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      tc.name,
				Namespace: tc.namespace,
			},
		}
		reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster.DeepCopy())
		result, err := reconciler.Reconcile(context.TODO(), request)
		assert.NoError(t, err)
		assert.Equalf(t, reconcile.Result{}, result, "[%s]: reconcile failed with non-watched resource", tc.label)

	}
}
func TestNonWatchedReconcileWithNoCephClusterType(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)
	cr := &api.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:       "storage-test",
			Namespace:  "storage-test-ns",
			Finalizers: []string{storageClusterFinalizer},
			Annotations: map[string]string{
				UninstallModeAnnotation: string(UninstallModeGraceful),
				CleanupPolicyAnnotation: string(CleanupPolicyDelete),
			},
		},
		Spec: api.StorageClusterSpec{
			Monitoring: &api.MonitoringSpec{
				ReconcileStrategy: string(ReconcileStrategyIgnore),
			},
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, cr, nodeList, infra, networkConfig)
	result, err := reconciler.Reconcile(context.TODO(), mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithTheCephClusterType(t *testing.T) {
	// TODO (leelavg): get back after upgrade is working
	// t.Skip("UID is not picked in reconcile even after supplying storageconsumer")
	testSkipPrometheusRules = true
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	cc.Status.State = rookCephv1.ClusterStateCreated
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)

	reconciler := createFakeStorageClusterReconciler(t, sc, cc, nodeList, infra, networkConfig)
	result, err := reconciler.Reconcile(context.TODO(), mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 6)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestStorageDeviceSets(t *testing.T) {
	scName := ""
	metadataScName := ""
	walScName := ""
	validScName := "gp2-csi"
	storageClassEBS := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gp2-csi",
		},
		Provisioner: string(EBS),
		Parameters: map[string]string{
			"type": "gp2-csi",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, storageClassEBS)

	testcases := []struct {
		label          string
		storageCluster *api.StorageCluster
		deviceSets     []api.StorageDeviceSet
		expectedError  error
	}{
		{
			label:          "Case 1",
			storageCluster: &api.StorageCluster{},
			deviceSets:     mockDeviceSets,
			expectedError:  nil,
		},
		{
			label:          "Case 2",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:                "mock-sds",
					Count:               3,
					DataPVCTemplate:     corev1.PersistentVolumeClaim{},
					MetadataPVCTemplate: mockMetaDataPVCTemplate.DeepCopy(),
					Portable:            true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified"),
		},
		{
			label:          "Case 3",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &scName,
						},
					},
					MetadataPVCTemplate: mockMetaDataPVCTemplate.DeepCopy(),
					Portable:            true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified"),
		},

		{
			label:          "Case 4",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:                "mock-sds",
					Count:               3,
					DataPVCTemplate:     *mockDataPVCTemplate.DeepCopy(),
					MetadataPVCTemplate: &corev1.PersistentVolumeClaim{},
					Portable:            true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified for metadataPVCTemplate"),
		},
		{
			label:          "Case 5",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:            "mock-sds",
					Count:           3,
					DataPVCTemplate: *mockDataPVCTemplate.DeepCopy(),
					MetadataPVCTemplate: &corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &metadataScName,
						},
					},
					Portable: true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified"),
		},
		{
			label:          "Case 6",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:            "mock-sds",
					Count:           3,
					DataPVCTemplate: *mockDataPVCTemplate.DeepCopy(),
					WalPVCTemplate: &corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &walScName,
						},
					},
					Portable: true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified for walPVCTemplate"),
		},
		{
			label:          "Case 7",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:            "mock-sds",
					Count:           3,
					DataPVCTemplate: *mockDataPVCTemplate.DeepCopy(),
					WalPVCTemplate:  &corev1.PersistentVolumeClaim{},
					Portable:        true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified for walPVCTemplate"),
		},
		{
			label:          "Case 8 - OSD size exceeds 16Ti",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &validScName,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("20Ti"),
								},
							},
						},
					},
					Portable: true,
				},
			},
			expectedError: fmt.Errorf("OSD size 20Ti exceeds maximum allowed size of 16Ti"),
		},
		{
			label:          "Case 9 - OSD size exactly 16Ti (should pass)",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &validScName,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("16Ti"),
								},
							},
						},
					},
					Portable: true,
				},
			},
			expectedError: nil,
		},
		{
			label:          "Case 10 - OSD size under 16Ti (should pass)",
			storageCluster: &api.StorageCluster{},
			deviceSets: []api.StorageDeviceSet{
				{
					Name:  "mock-sds",
					Count: 3,
					DataPVCTemplate: corev1.PersistentVolumeClaim{
						Spec: corev1.PersistentVolumeClaimSpec{
							StorageClassName: &validScName,
							Resources: corev1.VolumeResourceRequirements{
								Requests: corev1.ResourceList{
									corev1.ResourceStorage: resource.MustParse("10Ti"),
								},
							},
						},
					},
					Portable: true,
				},
			},
			expectedError: nil,
		},
	}

	for _, tc := range testcases {
		tc.storageCluster.Spec.StorageDeviceSets = tc.deviceSets
		err := reconciler.validateStorageDeviceSets(tc.storageCluster)
		if tc.expectedError == nil {
			assert.NoError(t, err)
			continue
		}
		assert.Error(t, err)
		assert.Containsf(t, err.Error(), tc.expectedError.Error(), "[%s]: failed to validate deviceSets", tc.label)
	}
}

func TestStorageClusterInitConditions(t *testing.T) {
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cc.Status.State = rookCephv1.ClusterStateCreated
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster.DeepCopy(), cc, nodeList, infra, networkConfig)
	result, err := reconciler.Reconcile(context.TODO(), mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 6)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestStorageClusterFinalizer(t *testing.T) {
	// TODO (leelavg): revisit after convergence
	t.Skip("this test is flaky as even without new updates it fails occasionally")
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
	}
	noobaaMock := &nbv1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster.DeepCopy(), noobaaMock.DeepCopy(), nodeList, infra, networkConfig)

	result, err := reconciler.Reconcile(context.TODO(), mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Ensure finalizer exists in the beginning
	sc := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	noobaa := &nbv1.NooBaa{}
	err = reconciler.Client.Get(context.TODO(), namespacedName, noobaa)
	assert.NoError(t, err)
	assert.Equal(t, noobaa.Name, noobaaMock.Name)

	// Issue a delete
	err = reconciler.Client.Delete(context.TODO(), sc)
	assert.NoError(t, err)

	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	err = reconciler.Client.Delete(context.TODO(), noobaa)
	assert.NoError(t, err)

	assert.Eventually(t, func() bool {
		result, err = reconciler.Reconcile(context.TODO(), mockStorageClusterRequest)
		if err != nil {
			assert.True(t, errors.IsNotFound(err), "error", err)
			return false
		}
		if result.IsZero() {
			return true
		}
		return false
	}, time.Minute, time.Second*2, "Timed out waiting for reconcile to complete")
	assert.Equal(t, result, reconcile.Result{})

	// Finalizer is removed
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	if err != nil {
		assert.True(t, errors.IsNotFound(err), "error", err)
	} else {
		assert.False(t, sc.ObjectMeta.DeletionTimestamp.IsZero())
		assert.Len(t, sc.ObjectMeta.GetFinalizers(), 0)
	}

	noobaa = &nbv1.NooBaa{}
	err = reconciler.Client.Get(context.TODO(), namespacedName, noobaa)
	assert.True(t, errors.IsNotFound(err))
}

func assertExpectedCondition(t *testing.T, conditions []conditionsv1.Condition) {
	expectedConditions := map[conditionsv1.ConditionType]corev1.ConditionStatus{
		api.ConditionReconcileComplete:    corev1.ConditionTrue,
		conditionsv1.ConditionAvailable:   corev1.ConditionFalse,
		conditionsv1.ConditionProgressing: corev1.ConditionTrue,
		conditionsv1.ConditionDegraded:    corev1.ConditionFalse,
		conditionsv1.ConditionUpgradeable: corev1.ConditionUnknown,
		api.ConditionVersionMismatch:      corev1.ConditionFalse,
	}
	for cType, status := range expectedConditions {
		found := assertCondition(conditions, cType, status)
		assert.True(t, found, "expected status condition not found", cType, status)
	}
}

func assertCondition(conditions []conditionsv1.Condition, conditionType conditionsv1.ConditionType, status corev1.ConditionStatus) bool {
	for _, objCondition := range conditions {
		if objCondition.Type == conditionType {
			if objCondition.Status == status {
				return true
			}
		}
	}
	return false
}

func createFakeStorageClusterReconciler(t *testing.T, obj ...runtime.Object) StorageClusterReconciler {
	sc := &api.StorageCluster{}
	scheme := createFakeScheme(t)
	name := mockStorageClusterRequest.NamespacedName.Name
	namespace := mockStorageClusterRequest.NamespacedName.Namespace
	consumer := &ocsv1a1.StorageConsumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      defaults.LocalStorageConsumerName,
			Namespace: namespace,
			UID:       "fake-uid",
		},
		Spec: ocsv1a1.StorageConsumerSpec{
			Enable: true,
		},
	}
	cfs := &rookCephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cephfilesystem", name),
			Namespace: namespace,
		},
		Status: &rookCephv1.CephFilesystemStatus{
			Phase: rookCephv1.ConditionType(statusutil.PhaseReady),
		},
	}
	cbp := &rookCephv1.CephBlockPool{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("%s-cephblockpool", name),
			Namespace: namespace,
		},
		Status: &rookCephv1.CephBlockPoolStatus{
			Phase: rookCephv1.ConditionType(statusutil.PhaseReady),
		},
	}
	verOcs, err := semver.Make(ocsversion.Version)
	if err != nil {
		panic(fmt.Sprintf("failed to parse version: %v", err))
	}
	csv := &opv1a1.ClusterServiceVersion{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("ocs-operator-%s", sc.Name),
			Namespace: namespace,
		},
		Spec: opv1a1.ClusterServiceVersionSpec{
			Version: opverion.OperatorVersion{Version: verOcs},
		},
	}
	rookCephMonSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "rook-ceph-mon", Namespace: namespace},
		Data: map[string][]byte{
			"fsid": []byte(cephFSID),
		},
	}
	clientConfigMap := &corev1.ConfigMap{}
	clientConfigMap.Name = ocsClientConfigMapName
	clientConfigMap.Namespace = namespace

	os.Setenv(providerAPIServerImage, "fake-image")
	os.Setenv(onboardingValidationKeysGeneratorImage, "fake-image")

	ocsProviderServiceDeployment := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName, Namespace: namespace},
	}

	ocsProviderService := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
		Status: corev1.ServiceStatus{
			LoadBalancer: corev1.LoadBalancerStatus{
				Ingress: []corev1.LoadBalancerIngress{{Hostname: "fake"}},
			},
		},
	}

	ocsProviderServiceSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: ocsProviderServerName},
	}

	obj = append(
		obj,
		createStorageClientCRD(),
		consumer,
		cbp,
		cfs,
		rookCephMonSecret,
		csv,
		clientConfigMap,
		ocsProviderService,
		ocsProviderServiceDeployment,
		ocsProviderServiceSecret,
		createRookCephOperatorCSV(namespace),
	)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).WithStatusSubresource(sc).Build()

	clusters, err := statusutil.GetClusters(context.TODO(), client)
	if err != nil {
		panic(fmt.Sprintf("Failed to get clusters %s", err.Error()))
	}

	operatorNamespace, err := statusutil.GetOperatorNamespace()
	if err != nil {
		operatorNamespace = "openshift-storage"
	}
	//Update the deployment replicas to 1
	ocsProviderServiceDeployment.Status.AvailableReplicas = 1
	err = client.Status().Update(context.TODO(), ocsProviderServiceDeployment)
	assert.NoError(t, err)

	frecorder := record.NewFakeRecorder(1024)
	reporter := statusutil.NewEventReporter(frecorder)

	availCrds := map[string]bool{
		StorageClientCrdName: true,
	}

	return StorageClusterReconciler{
		recorder:          reporter,
		Client:            client,
		Scheme:            scheme,
		OperatorCondition: newStubOperatorCondition(),
		Log:               logf.Log.WithName("controller_storagecluster_test"),
		clusters:          clusters,
		OperatorNamespace: operatorNamespace,
		AvailableCrds:     availCrds,
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme := runtime.NewScheme()

	err := api.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to build scheme")
	}
	err = batchv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to add batchv1 to scheme")
	}
	err = corev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add corev1 scheme")
	}
	err = storagev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add storagev1 scheme")
	}
	err = rookCephv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add rookCephv1 scheme")
	}
	err = openshiftv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add openshiftv1 scheme")
	}
	err = snapapi.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add volume-snapshot scheme")
	}
	err = groupsnapapi.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add volume-group-snapshot scheme")
	}
	err = odfgsapiv1b1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add odf-volume-group-snapshot scheme")
	}
	err = monitoringv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add monitoringv1 scheme")
	}
	err = configv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add configv1 scheme")
	}
	err = extv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add extv1 scheme")
	}
	err = routev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add routev1 scheme")
	}

	err = nbv1.SchemeBuilder.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add nbv1 scheme")
	}

	err = appsv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add appsv1 scheme")
	}

	err = quotav1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add quotav1 scheme")
	}

	err = ocsv1alpha1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsv1alpha1 scheme")
	}

	err = ocsclientv1a1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add ocsclientv1a1 scheme")
	}

	err = opv1a1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to add opv1a1 to scheme")
	}

	err = rbacv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to add rbacv1 to scheme")
	}

	err = csiaddonsv1alpha1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "unable to add rbacv1 to scheme")
	}

	return scheme
}

// TestStorageClusterOnMultus tests if multus configurations in StorageCluster are successfully applied to CephClusterCR
func TestStorageClusterOnMultus(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	cases := []struct {
		testCase  string
		publicNW  string
		clusterNW string
		cr        *api.StorageCluster
	}{
		{
			// When only public network is specified.
			testCase:  "public",
			publicNW:  "public-network",
			clusterNW: "",
		},
		{
			// When only cluster network is specified, this will be an error case .
			testCase:  "cluster",
			publicNW:  "",
			clusterNW: "cluster-network",
		},
		{
			// When both public and cluster network are specified.
			testCase:  "both",
			publicNW:  "public-network",
			clusterNW: "cluster-network",
		},
		{
			// When public network and cluster network is empty, this will be an error case.
			testCase:  "none",
			publicNW:  "",
			clusterNW: "",
		},
		{
			// When Network is not specified
			testCase: "default",
		},
	}

	for _, c := range cases {
		c.cr = createDefaultStorageCluster()
		if c.testCase != "default" {
			c.cr.Spec.Network = &rookCephv1.NetworkSpec{
				Provider: networkProvider,
				Selectors: map[rookCephv1.CephNetworkType]string{
					rookCephv1.CephNetworkPublic:  c.publicNW,
					rookCephv1.CephNetworkCluster: c.clusterNW,
				},
			}
		}

		reconciler := createFakeInitializationStorageClusterReconciler(t)
		_ = reconciler.Client.Create(context.TODO(), c.cr)
		result, err := reconciler.Reconcile(context.TODO(), request)
		if c.testCase != "default" {
			validMultus := validateMultusSelectors(c.cr.Spec.Network.Selectors)
			if validMultus != nil {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, reconcile.Result{}, result)
				assertCephClusterNetwork(t, reconciler, c.cr, request)
			}
		}
	}
}

func assertCephClusterNetwork(t assert.TestingT, reconciler StorageClusterReconciler, cr *api.StorageCluster, request reconcile.Request) {
	request.Name = "ocsinit-cephcluster"
	cephCluster := newCephCluster(&reconciler, cr, nil)
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, cephCluster)
	assert.NoError(t, err)
	if cr.Spec.Network == nil {
		assert.Equal(t, "", cephCluster.Spec.Network.Provider)
		assert.Nil(t, cephCluster.Spec.Network.Selectors)
	}
}

func getSemVer(version string, majorDiff uint64, islower bool) string {
	sv, err := semver.Make(version)
	if err != nil {
		return version
	}
	if islower {
		sv.Major = sv.Major - majorDiff
	} else {
		sv.Major = sv.Major + majorDiff
	}

	return sv.String()
}

type ListenServer struct {
	Listener net.Listener
}

func NewListenServer(endpoint string) (*ListenServer, error) {
	ls := &ListenServer{}

	rxp := regexp.MustCompile(`^http[s]?://`)
	// remove any http or https protocols from the endpoint string
	endpoint = rxp.ReplaceAllString(endpoint, "")
	ln, err := net.Listen("tcp4", endpoint)
	if err != nil {
		return nil, err
	}

	ls.Listener = ln

	go func(ln net.Listener) {
		for {
			conn, err := ln.Accept()
			if err != nil {
				break
			}
			conn.Close()
		}
	}(ls.Listener)

	return ls, nil

}

func startServerAt(t *testing.T, endpoint string) {
	ls, err := NewListenServer(endpoint)
	if err != nil {
		t.Logf("Failed to start ListenServer: %v", err)
		return
	}
	t.Cleanup(func() { ls.Listener.Close() })

}
