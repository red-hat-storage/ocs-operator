package storagecluster

import (
	"context"
	"fmt"
	"testing"

	"github.com/blang/semver"
	"github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	configv1 "github.com/openshift/api/config/v1"
	consolev1 "github.com/openshift/api/console/v1"
	openshiftv1 "github.com/openshift/api/template/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"

	snapapi "github.com/kubernetes-csi/external-snapshotter/v2/pkg/apis/volumesnapshot/v1beta1"
	rookCephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	v1 "github.com/rook/rook/pkg/apis/rook.io/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	storagev1 "k8s.io/api/storage/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	k8sVersion "k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/openshift/ocs-operator/api/v1"
	"github.com/openshift/ocs-operator/controllers/defaults"
	statusutil "github.com/openshift/ocs-operator/controllers/util"
	"github.com/openshift/ocs-operator/version"
)

const (
	zoneTopologyLabel   = "failure-domain.kubernetes.io/zone"
	regionTopologyLabel = "failure-domain.kubernetes.io/region"
	hostnameLabel       = "kubernetes.io/hostname"
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
	},
}

var mockCephCluster = &rookCephv1.CephCluster{
	ObjectMeta: metav1.ObjectMeta{
		Name:      generateNameForCephCluster(mockStorageCluster),
		Namespace: mockStorageCluster.Namespace,
	},
}

var mockCephClusterNamespacedName = types.NamespacedName{
	Name:      generateNameForCephCluster(mockStorageCluster),
	Namespace: mockStorageCluster.Namespace,
}

var storageClassName = "gp2"
var storageClassName2 = "managed-premium"
var fakestorageClassName = "st1"
var volMode = corev1.PersistentVolumeBlock
var annotations = map[string]string{
	"crushDeviceClass": "",
}

var mockDataPVCTemplate = corev1.PersistentVolumeClaim{
	ObjectMeta: metav1.ObjectMeta{
		Annotations: annotations,
	},
	Spec: corev1.PersistentVolumeClaimSpec{
		AccessModes: []corev1.PersistentVolumeAccessMode{corev1.ReadWriteOnce},
		Resources: corev1.ResourceRequirements{
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
		Resources: corev1.ResourceRequirements{
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
		Resources: corev1.ResourceRequirements{
			Requests: corev1.ResourceList{
				corev1.ResourceStorage: resource.MustParse("1Ti"),
			},
		},
		StorageClassName: &storageClassName,
		VolumeMode:       &volMode,
	},
}

var mockDeviceSets = []api.StorageDeviceSet{
	{
		Name:                "mock-sds",
		Count:               3,
		DataPVCTemplate:     mockDataPVCTemplate,
		MetadataPVCTemplate: mockMetaDataPVCTemplate,
		WalPVCTemplate:      mockWalPVCTemplate,
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
					hostnameLabel:            "node1",
					zoneTopologyLabel:        "zone1",
					defaults.NodeAffinityKey: "",
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
			expectedVersion: version.Version,
			errorExpected:   false,
		},
		{
			label: "Case 2", // a lower version is provided in the storagecluster
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test",
					Namespace: "storage-test-ns",
				},
				Spec: api.StorageClusterSpec{
					Version: getSemVer(version.Version, 1, true),
				},
			},
			expectedVersion: version.Version,
			errorExpected:   false,
		},
		{
			label: "Case 3", // a higher version is provided in the storagecluster
			storageCluster: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test",
					Namespace: "storage-test-ns",
				},
				Spec: api.StorageClusterSpec{
					Version: getSemVer(version.Version, 1, false),
				},
			},
			expectedVersion: version.Version,
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
		assert.Equalf(t, tc.expectedVersion, tc.storageCluster.Spec.Version, "[%q]: failed to get correct version", tc.label)
	}

}

func TestThrottleStorageDevices(t *testing.T) {
	testcases := []struct {
		label          string
		storageClass   *storagev1.StorageClass
		deviceSets     []api.StorageDeviceSet
		storageCluster *api.StorageCluster
		expectedSpeed  diskSpeed
		platform       *Platform
	}{
		{
			label: "Case 1", // storageclass is gp2 or io1
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gp2",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "gp2",
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
			label: "Case 2", // storageclass is neither gp2 nor io1
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
							StorageClassName: &fakestorageClassName,
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
			label: "Case 5", // storageclass is gp2 but deviceType ssd
			storageClass: &storagev1.StorageClass{
				ObjectMeta: metav1.ObjectMeta{
					Name: "gp2",
				},
				Provisioner: string(EBS),
				Parameters: map[string]string{
					"type": "gp2",
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
			label: "Case 6", // storageclass is neither gp2 nor io1 but deviceType nvme
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
							StorageClassName: &fakestorageClassName,
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
			label: "Case 7", // storageclass is neither gp2 nor io1 but platform is Azure
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
							StorageClassName: &fakestorageClassName,
						},
					},
					Portable: true,
				},
			},
			platform:       &Platform{platform: configv1.AzurePlatformType},
			storageCluster: &api.StorageCluster{},
			expectedSpeed:  diskSpeedFast,
		},
	}

	for _, tc := range testcases {
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster, tc.storageClass)
		if tc.platform != nil {
			reconciler.platform = tc.platform
		}
		for _, ds := range tc.deviceSets {
			actualSpeed, err := reconciler.checkTuneStorageDevices(ds)
			assert.NoError(t, err)
			assert.Equalf(t, tc.expectedSpeed, actualSpeed, "[%q]: failed to get expected output", tc.label)
		}
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
			label:           "Case 4", // storageCluster2 should be active as there are no other storageClusters available
			storageCluster1: &api.StorageCluster{},
			storageCluster2: &api.StorageCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "storage-test-b",
					Namespace: "storage-test-ns",
				},
			},
			isActive: true,
		},
	}

	for _, tc := range testcases {
		reconciler := createFakeStorageClusterReconciler(t, tc.storageCluster1, tc.storageCluster2)
		actual, err := reconciler.isActiveStorageCluster(tc.storageCluster2)
		assert.NoError(t, err)
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
		reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster)
		result, err := reconciler.Reconcile(request)
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
			Name:      "storage-test",
			Namespace: "storage-test-ns",
		},
	}

	reconciler := createFakeStorageClusterReconciler(t, cr, nodeList, infra)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)
}

func TestNonWatchedReconcileWithTheCephClusterType(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	cc := &rookCephv1.CephCluster{}
	mockCephCluster.DeepCopyInto(cc)
	cc.Status.State = rookCephv1.ClusterStateCreated
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)
	sc := &api.StorageCluster{}
	mockStorageCluster.DeepCopyInto(sc)

	reconciler := createFakeStorageClusterReconciler(t, sc, cc, nodeList, infra)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestStorageDeviceSets(t *testing.T) {
	scName := ""
	metadataScName := ""
	walScName := ""
	storageClassEBS := &storagev1.StorageClass{
		ObjectMeta: metav1.ObjectMeta{
			Name: "gp2",
		},
		Provisioner: string(EBS),
		Parameters: map[string]string{
			"type": "gp2",
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
					MetadataPVCTemplate: mockMetaDataPVCTemplate,
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
					MetadataPVCTemplate: mockMetaDataPVCTemplate,
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
					DataPVCTemplate:     mockDataPVCTemplate,
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
					DataPVCTemplate: mockDataPVCTemplate,
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
					DataPVCTemplate: mockDataPVCTemplate,
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
					DataPVCTemplate: mockDataPVCTemplate,
					WalPVCTemplate:  &corev1.PersistentVolumeClaim{},
					Portable:        true,
				},
			},
			expectedError: fmt.Errorf("no StorageClass specified for walPVCTemplate"),
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

	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, cc, nodeList, infra)
	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	actual := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, actual)
	assert.NoError(t, err)
	assert.NotEmpty(t, actual.Status.Conditions)
	assert.Len(t, actual.Status.Conditions, 5)

	assertExpectedCondition(t, actual.Status.Conditions)
}

func TestStorageClusterFinalizer(t *testing.T) {
	nodeList := &corev1.NodeList{}
	mockNodeList.DeepCopyInto(nodeList)
	infra := &configv1.Infrastructure{}
	mockInfrastructure.DeepCopyInto(infra)
	namespacedName := types.NamespacedName{
		Name:      "noobaa",
		Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
	}
	noobaaMock := &v1alpha1.NooBaa{
		ObjectMeta: metav1.ObjectMeta{
			Name:      namespacedName.Name,
			Namespace: mockStorageClusterRequest.NamespacedName.Namespace,
			SelfLink:  "/api/v1/namespaces/openshift-storage/noobaa/noobaa",
		},
	}
	reconciler := createFakeStorageClusterReconciler(t, mockStorageCluster, noobaaMock, nodeList, infra)

	result, err := reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Ensure finalizer exists in the beginning
	sc := &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	noobaa := &v1alpha1.NooBaa{}
	err = reconciler.Client.Get(context.TODO(), namespacedName, noobaa)
	assert.NoError(t, err)
	assert.Equal(t, noobaa.Name, noobaaMock.Name)

	// Issue a delete
	now := metav1.Now()
	sc.SetDeletionTimestamp(&now)
	err = reconciler.Client.Update(context.TODO(), sc)
	assert.NoError(t, err)

	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 1)

	err = reconciler.Client.Delete(context.TODO(), noobaa)
	assert.NoError(t, err)

	result, err = reconciler.Reconcile(mockStorageClusterRequest)
	assert.NoError(t, err)
	assert.Equal(t, reconcile.Result{}, result)

	// Finalizer is removed
	sc = &api.StorageCluster{}
	err = reconciler.Client.Get(context.TODO(), mockStorageClusterRequest.NamespacedName, sc)
	assert.NoError(t, err)
	assert.Len(t, sc.ObjectMeta.GetFinalizers(), 0)

	noobaa = &v1alpha1.NooBaa{}
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
	scheme := createFakeScheme(t)
	client := fake.NewFakeClientWithScheme(scheme, obj...)

	return StorageClusterReconciler{
		Client:        client,
		Scheme:        scheme,
		serverVersion: &k8sVersion.Info{},
		Log:           logf.Log.WithName("controller_storagecluster_test"),
		platform:      &Platform{platform: configv1.NonePlatformType},
	}
}

func createFakeScheme(t *testing.T) *runtime.Scheme {
	scheme, err := api.SchemeBuilder.Build()
	if err != nil {
		assert.Fail(t, "unable to build scheme")
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
	err = monitoringv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add monitoringv1 scheme")
	}
	err = configv1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add configv1 scheme")
	}
	err = consolev1.AddToScheme(scheme)
	if err != nil {
		assert.Fail(t, "failed to add consolev1 scheme")
	}
	return scheme
}

//nolint //ignoring err checks as causing failures
func TestMonCountChange(t *testing.T) {
	for nodeCount := 0; nodeCount <= 10; nodeCount++ {
		monCountExpected := defaults.DefaultMonCount
		monCountActual := getMonCount(nodeCount, false)
		assert.Equal(t, monCountExpected, monCountActual)
	}
}

// TestStorageClusterOnMultus tests if multus configurations in StorageCluster are successfully applied to CephClusterCR
func TestStorageClusterOnMultus(t *testing.T) {
	request := reconcile.Request{
		NamespacedName: types.NamespacedName{
			Name:      "ocsinit",
			Namespace: "",
		},
	}
	platform := &Platform{platform: configv1.NonePlatformType}
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
			c.cr.Spec.Network = &v1.NetworkSpec{
				Provider: networkProvider,
				Selectors: map[string]string{
					"public":  c.publicNW,
					"cluster": c.clusterNW,
				},
			}
		}
		reconciler := createFakeInitializationStorageClusterReconcilerWithPlatform(t, platform)
		_ = reconciler.Client.Create(context.TODO(), c.cr)
		result, err := reconciler.Reconcile(request)
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
	serverVersion := &k8sVersion.Info{}
	request.Name = "ocsinit-cephcluster"
	cephCluster := newCephCluster(cr, "", 3, serverVersion, nil, log)
	err := reconciler.Client.Get(context.TODO(), request.NamespacedName, cephCluster)
	assert.NoError(t, err)
	if cr.Spec.Network == nil {
		assert.Equal(t, "", cephCluster.Spec.Network.NetworkSpec.Provider)
		assert.Nil(t, cephCluster.Spec.Network.NetworkSpec.Selectors)
	} else {
		assert.Equal(t, *cr.Spec.Network, cephCluster.Spec.Network.NetworkSpec)
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
