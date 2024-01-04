package storagecluster

import (
	"context"
	"testing"

	configv1 "github.com/openshift/api/config/v1"
	quotav1 "github.com/openshift/api/quota/v1"
	api "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	k8sVersion "k8s.io/apimachinery/pkg/version"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var mockStorageClassName = "ceph-rbd"
var mockQuantity1T = resource.MustParse("1Ti")
var mockQuantity2T = resource.MustParse("2Ti")
var mockStorageDeviceSets = []api.StorageDeviceSet{
	{
		Name:    "mock-storagecluster-clusterresourcequota",
		Count:   3,
		Replica: 2,
		DataPVCTemplate: corev1.PersistentVolumeClaim{
			Spec: corev1.PersistentVolumeClaimSpec{
				StorageClassName: &mockStorageClassName,
				Resources: corev1.VolumeResourceRequirements{
					Requests: corev1.ResourceList{
						corev1.ResourceStorage: mockQuantity1T,
					},
				},
			},
		},
		Portable:   false,
		DeviceType: "ssd",
	},
}
var mockOverprovisionControl = []api.OverprovisionControlSpec{
	{
		StorageClassName: mockStorageClassName,
		QuotaName:        "quota1",
		Capacity:         mockQuantity2T,
		Selector: quotav1.ClusterResourceQuotaSelector{
			LabelSelector: &metav1.LabelSelector{
				MatchExpressions: []metav1.LabelSelectorRequirement{
					{
						Key:      "storagequota_test",
						Values:   []string{"test1"},
						Operator: metav1.LabelSelectorOpExists,
					},
				},
			},
		},
	},
}

func TestStorageQuotaEnsureCreatedDeleted(t *testing.T) {
	testcases := []struct {
		label          string
		storageCluster *api.StorageCluster
	}{
		{
			label: "Case 1",
			storageCluster: &api.StorageCluster{
				Spec: api.StorageClusterSpec{
					OverprovisionControl: []api.OverprovisionControlSpec{
						{
							StorageClassName: "ceph-rbd",
							QuotaName:        "quota2",
							Capacity:         mockQuantity1T,
						},
					},
				},
			},
		},
	}

	for _, tc := range testcases {
		var obj ocsStorageQuota
		r := createFakeStorageClusterWithQuotaReconciler(t)
		_, err := obj.ensureCreated(r, tc.storageCluster)
		assert.NoError(t, err)
		assert.Equal(t, len(listStorageQuotas(t, r)), len(tc.storageCluster.Spec.OverprovisionControl))
		_, err = obj.ensureDeleted(r, tc.storageCluster)
		assert.NoError(t, err)
		assert.Equal(t, len(listStorageQuotas(t, r)), 0)
	}
}
func TestStorageQuotaWithFullStorageCluster(t *testing.T) {
	r := createFakeStorageClusterWithQuotaReconciler(t)
	sc := createStorageClusterWithOverprovision()

	var obj ocsStorageQuota
	_, err := obj.ensureCreated(r, sc)
	assert.NoError(t, err)
	assert.Equal(t, len(listStorageQuotas(t, r)), len(sc.Spec.OverprovisionControl))
	_, err = obj.ensureDeleted(r, sc)
	assert.NoError(t, err)
	assert.Equal(t, len(listStorageQuotas(t, r)), 0)
}

func createStorageClusterWithOverprovision() *api.StorageCluster {
	sc := mockStorageCluster.DeepCopy()
	sc.Spec.StorageDeviceSets = []api.StorageDeviceSet{}
	for _, sd := range mockStorageDeviceSets {
		sc.Spec.StorageDeviceSets = append(sc.Spec.StorageDeviceSets, *sd.DeepCopy())
	}
	sc.Spec.OverprovisionControl = []api.OverprovisionControlSpec{}
	for _, opc := range mockOverprovisionControl {
		sc.Spec.OverprovisionControl = append(sc.Spec.OverprovisionControl, *opc.DeepCopy())
	}
	return sc
}

func createFakeStorageClusterWithQuotaReconciler(t *testing.T, obj ...runtime.Object) *StorageClusterReconciler {
	sc := &api.StorageCluster{}
	scheme := createFakeScheme(t)
	client := fake.NewClientBuilder().WithScheme(scheme).WithRuntimeObjects(obj...).WithStatusSubresource(sc).Build()

	return &StorageClusterReconciler{
		Client:        client,
		Scheme:        scheme,
		serverVersion: &k8sVersion.Info{},
		Log:           logf.Log.WithName("storagequota_test"),
		platform:      &Platform{platform: configv1.NonePlatformType},
	}
}

func listStorageQuotas(t *testing.T, r *StorageClusterReconciler) []quotav1.ClusterResourceQuota {
	ls := &quotav1.ClusterResourceQuotaList{}
	err := r.Client.List(context.TODO(), ls)
	assert.NoError(t, err)
	return ls.Items
}
