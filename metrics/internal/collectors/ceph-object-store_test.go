package collectors

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/red-hat-storage/ocs-operator/v4/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var (
	mockCephObjectStore1 = cephv1.CephObjectStore{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ceph.rook.io/v1",
			Kind:       "CephObjectStore",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephObjectStore-1",
			Namespace: "openshift-storage",
		},
		Spec:   cephv1.ObjectStoreSpec{},
		Status: &cephv1.ObjectStoreStatus{},
	}
	mockCephObjectStore2 = cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephObjectStore-2",
			Namespace: "openshift-storage",
		},
		Spec:   cephv1.ObjectStoreSpec{},
		Status: &cephv1.ObjectStoreStatus{},
	}
	mockCephObjectStore3 = cephv1.CephObjectStore{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephObjectStore-3",
			Namespace: "default",
		},
		Spec:   cephv1.ObjectStoreSpec{},
		Status: &cephv1.ObjectStoreStatus{},
	}
)

func (c *CephObjectStoreCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockCephObjectStoreCollector(t *testing.T, mockOpts *options.Options) (mockCephObjectStoreCollector *CephObjectStoreCollector) {
	setKubeConfig(t)
	mockCephObjectStoreCollector = NewCephObjectStoreCollector(mockOpts)
	assert.NotNil(t, mockCephObjectStoreCollector)
	return
}

func TestNewCephObjectStoreCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test CephObjectStoreCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockCephObjectStoreCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
	}
}

func TestGetAllObjectStores(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephObjectStoreCollector := getMockCephObjectStoreCollector(t, mockOpts)

	tests := Tests{
		{
			name: "CephObjectStore doesn't exist",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{},
			// []*cephv1.CephObjectStore(nil) is not DeepEqual to []*cephv1.CephObjectStore{}
			// getAllObjectStores returns []*cephv1.CephObjectStore(nil) if no CephObjectStore is present
			wantObjects: []runtime.Object(nil),
		},
		{
			name: "One CephObjectStore exists",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephObjectStore1,
			},
			wantObjects: []runtime.Object{
				&mockCephObjectStore1,
			},
		},
		{
			name: "Two CephObjectStores exists",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
			},
			wantObjects: []runtime.Object{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
			},
		},
		{
			name: "One CephObjectStores exists in disallowed namespace",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
				&mockCephObjectStore3,
			},
			wantObjects: []runtime.Object{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
			},
		},
	}
	for _, tt := range tests {
		setInformer(t, tt.inputObjects, cephObjectStoreCollector)
		gotCephObjectStores := getAllObjectStores(tt.args.lister.(cephv1listers.CephObjectStoreLister), tt.args.namespaces)
		assert.Len(t, gotCephObjectStores, len(tt.wantObjects))
		for _, obj := range gotCephObjectStores {
			assert.Contains(t, tt.wantObjects, obj)
		}
		resetInformer(t, tt.inputObjects, cephObjectStoreCollector)
	}
}

func TestCollectObjectStoreHealth(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephObjectStoreCollector := getMockCephObjectStoreCollector(t, mockOpts)
	mockInfo := map[string]string{
		"endpoint": "http://www.endpoint.mock:1234",
	}

	objConnected := mockCephObjectStore1.DeepCopy()
	objConnected.Name = objConnected.Name + string(cephv1.ConditionConnected)
	objConnected.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionConnected,
		Info:    mockInfo,
		Message: "",
	}

	objProgressing := mockCephObjectStore1.DeepCopy()
	objProgressing.Name = objProgressing.Name + string(cephv1.ConditionProgressing)
	objProgressing.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionProgressing,
		Info:    mockInfo,
		Message: "",
	}

	objFailure := mockCephObjectStore1.DeepCopy()
	objFailure.Name = objFailure.Name + string(cephv1.ConditionFailure)
	objFailure.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionFailure,
		Info:    mockInfo,
		Message: "",
	}

	objConnecting := mockCephObjectStore1.DeepCopy()
	objConnecting.Name = objConnecting.Name + string(cephv1.ConditionConnecting)
	objConnecting.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionConnecting,
		Info:    mockInfo,
		Message: "",
	}

	objReady := mockCephObjectStore1.DeepCopy()
	objReady.Name = objReady.Name + string(cephv1.ConditionReady)
	objReady.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionReady,
		Info:    mockInfo,
		Message: "",
	}

	objDeleting := mockCephObjectStore1.DeepCopy()
	objDeleting.Name = objDeleting.Name + string(cephv1.ConditionDeleting)
	objDeleting.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionDeleting,
		Info:    mockInfo,
		Message: "",
	}

	objDeletionBlocked := mockCephObjectStore1.DeepCopy()
	objDeletionBlocked.Name = objDeletionBlocked.Name + string(cephv1.ConditionDeletionIsBlocked)
	objDeletionBlocked.Status = &cephv1.ObjectStoreStatus{
		Phase:   cephv1.ConditionDeletionIsBlocked,
		Info:    mockInfo,
		Message: "",
	}

	tests := Tests{
		{
			name: "Collect Ceph Object Store health metrics",
			args: args{
				objects: []runtime.Object{
					objConnected,
					objProgressing,
					objFailure,
					objConnecting,
					objReady,
					objDeleting,
					objDeletionBlocked,
				},
			},
		},
		{
			name: "Empty CephObjectStores",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}
	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var cephObjectStores []*cephv1.CephObjectStore
			for _, obj := range tt.args.objects {
				cephObjectStores = append(cephObjectStores, obj.(*cephv1.CephObjectStore))
			}
			cephObjectStoreCollector.collectObjectStoreHealth(cephObjectStores, ch)
			close(ch)
		}()

		for m := range ch {
			assert.Contains(t, m.Desc().String(), "health_status")
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)
			labels := metric.GetLabel()
			for _, label := range labels {
				if *label.Name == "name" {
					if *label.Value == objConnected.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(0))
					} else if *label.Value == objProgressing.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(1))
					} else if *label.Value == objFailure.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(2))
					} else if *label.Value == objConnecting.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(3))
					} else if *label.Value == objReady.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(4))
					} else if *label.Value == objDeleting.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(5))
					} else if *label.Value == objDeletionBlocked.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(6))
					}
				} else if *label.Name == "namespace" {
					assert.Contains(t, cephObjectStoreCollector.AllowedNamespaces, *label.Value)
				} else if *label.Name == "rgw_endpoint" {
					assert.Equal(t, mockInfo["endpoint"], *label.Value)
				}
			}
		}
	}
}
