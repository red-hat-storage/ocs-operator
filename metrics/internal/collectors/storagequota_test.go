package collectors

import (
	"testing"

	quotav1 "github.com/openshift/api/quota/v1"
	"github.com/openshift/ocs-operator/metrics/internal/options"
	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	testQuantity1T            = resource.MustParse("1Ti")
	testQuantity2T            = resource.MustParse("2Ti")
	mockClusterResourceQuota1 = quotav1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockStorageQuota-1",
			Namespace: "openshift-storage",
		},
		Spec: quotav1.ClusterResourceQuotaSpec{
			Selector: quotav1.ClusterResourceQuotaSelector{},
			Quota: corev1.ResourceQuotaSpec{
				Hard:          corev1.ResourceList{corev1.ResourceStorage: testQuantity1T},
				Scopes:        []corev1.ResourceQuotaScope{},
				ScopeSelector: &corev1.ScopeSelector{},
			},
		},
		Status: quotav1.ClusterResourceQuotaStatus{
			Total: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{corev1.ResourceStorage: testQuantity1T},
				Used: corev1.ResourceList{},
			},
			Namespaces: []quotav1.ResourceQuotaStatusByNamespace{},
		},
	}
	mockClusterResourceQuota2 = quotav1.ClusterResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockStorageQuota-2",
			Namespace: "openshift-storage",
		},
		Spec: quotav1.ClusterResourceQuotaSpec{
			Selector: quotav1.ClusterResourceQuotaSelector{},
			Quota: corev1.ResourceQuotaSpec{
				Hard:          corev1.ResourceList{corev1.ResourceStorage: testQuantity2T},
				Scopes:        []corev1.ResourceQuotaScope{},
				ScopeSelector: &corev1.ScopeSelector{},
			},
		},
		Status: quotav1.ClusterResourceQuotaStatus{
			Total: corev1.ResourceQuotaStatus{
				Hard: corev1.ResourceList{corev1.ResourceStorage: testQuantity2T},
				Used: corev1.ResourceList{corev1.ResourceStorage: testQuantity1T},
			},
			Namespaces: []quotav1.ResourceQuotaStatusByNamespace{},
		},
	}
)

func getMockStorageQuotaCollector(t *testing.T, mockOpts *options.Options) (mockStorageQuotaCollector *StorageQuotaCollector) {
	setKubeConfig(t)
	mockStorageQuotaCollector = NewStorageQuotaCollector(mockOpts)
	assert.NotNil(t, mockStorageQuotaCollector)
	return
}

func setInformerStoreQuota(t *testing.T, objs []*quotav1.ClusterResourceQuota, mockStorageQuotaCollector *StorageQuotaCollector) {
	for _, obj := range objs {
		err := mockStorageQuotaCollector.Informer.GetStore().Add(obj)
		assert.Nil(t, err)
	}
}

func resetInformerStoreQuota(t *testing.T, objs []*quotav1.ClusterResourceQuota, mockStorageQuotaCollector *StorageQuotaCollector) {
	for _, obj := range objs {
		err := mockStorageQuotaCollector.Informer.GetStore().Delete(obj)
		assert.Nil(t, err)
	}
}

func TestNewStorageQuotaCollector(t *testing.T) {
	type args struct {
		opts *options.Options
	}
	tests := []struct {
		name string
		args args
	}{
		{
			name: "Test StorageQuotaCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockStorageQuotaCollector(t, tt.args.opts)
		assert.NotNil(t, got.Informer)
	}
}

func TestListStorageQuotas(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	storageQuotaCollector := getMockStorageQuotaCollector(t, mockOpts)

	tests := []struct {
		name                      string
		inputClusterResourceQuota []*quotav1.ClusterResourceQuota
		storageQuotaHard          []storageQuotaInfo
		storageQuotaUsed          []storageQuotaInfo
	}{
		{
			name:                      "ClusterResourceQuota doesn't exist",
			inputClusterResourceQuota: []*quotav1.ClusterResourceQuota{},
			storageQuotaHard:          []storageQuotaInfo{},
			storageQuotaUsed:          []storageQuotaInfo{},
		},
		{
			name:                      "ClusterResourceQuota with HARD only",
			inputClusterResourceQuota: []*quotav1.ClusterResourceQuota{&mockClusterResourceQuota1},
			storageQuotaHard:          []storageQuotaInfo{{Quantity: testQuantity1T}},
			storageQuotaUsed:          []storageQuotaInfo{},
		},
		{
			name:                      "ClusterResourceQuota with HARD/USED pair",
			inputClusterResourceQuota: []*quotav1.ClusterResourceQuota{&mockClusterResourceQuota2},
			storageQuotaHard:          []storageQuotaInfo{{Quantity: testQuantity2T}},
			storageQuotaUsed:          []storageQuotaInfo{{Quantity: testQuantity1T}},
		},
	}
	for _, tt := range tests {
		setInformerStoreQuota(t, tt.inputClusterResourceQuota, storageQuotaCollector)
		storageQuotaHard, storageQuotaUsed := storageQuotaCollector.listStorageQuotas()
		assert.Len(t, storageQuotaHard, len(tt.storageQuotaHard))
		assert.Len(t, storageQuotaUsed, len(tt.storageQuotaUsed))
		for idx := range tt.storageQuotaHard {
			hard1, ok1 := storageQuotaHard[idx].Quantity.AsInt64()
			hard2, ok2 := tt.storageQuotaHard[idx].Quantity.AsInt64()
			assert.True(t, ok1)
			assert.True(t, ok2)
			assert.Equal(t, hard1, hard2)
		}
		for idx := range tt.storageQuotaUsed {
			used1, ok1 := storageQuotaUsed[idx].Quantity.AsInt64()
			used2, ok2 := tt.storageQuotaUsed[idx].Quantity.AsInt64()
			assert.True(t, ok1)
			assert.True(t, ok2)
			assert.Equal(t, used1, used2)
		}
		resetInformerStoreQuota(t, tt.inputClusterResourceQuota, storageQuotaCollector)
	}
}
