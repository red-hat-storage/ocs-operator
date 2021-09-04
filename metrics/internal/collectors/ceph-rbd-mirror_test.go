package collectors

import (
	"testing"

	"github.com/red-hat-storage/ocs-operator/metrics/internal/options"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var (
	mockCephRBDMirror1 = cephv1.CephRBDMirror{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "ceph.rook.io/v1",
			Kind:       "CephRBDMirror",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephRBDMirror-1",
			Namespace: "openshift-storage",
		},
		Spec:   cephv1.RBDMirroringSpec{},
		Status: &cephv1.Status{},
	}
	mockCephRBDMirror2 = cephv1.CephRBDMirror{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephRBDMirror-2",
			Namespace: "openshift-storage",
		},
		Spec:   cephv1.RBDMirroringSpec{},
		Status: &cephv1.Status{},
	}
)

func (c *CephRBDMirrorCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockCephRBDMirrorCollector(t *testing.T, mockOpts *options.Options) (mockCephRBDMirrorCollector *CephRBDMirrorCollector) {
	setKubeConfig(t)
	mockCephRBDMirrorCollector = NewCephRBDMirrorCollector(mockOpts)
	assert.NotNil(t, mockCephRBDMirrorCollector)
	return
}

func TestNewCephRBDMirrorCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test CephRBDMirrorCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockCephRBDMirrorCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
	}
}

func TestGetAllRBDMirrors(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephRBDMirrorCollector := getMockCephRBDMirrorCollector(t, mockOpts)

	tests := Tests{
		{
			name: "CephRBDMirror doesn't exist",
			args: args{
				lister:     cephv1listers.NewCephRBDMirrorLister(cephRBDMirrorCollector.Informer.GetIndexer()),
				namespaces: cephRBDMirrorCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{},
			// []*cephv1.CephRBDMirror(nil) is not DeepEqual to []*cephv1.CephRBDMirror{}
			// GetAllRBDMirrors returns []*cephv1.CephRBDMirror(nil) if no CephRBDMirror is present
			wantObjects: []runtime.Object(nil),
		},
		{
			name: "One CephRBDMirror exists",
			args: args{
				lister:     cephv1listers.NewCephRBDMirrorLister(cephRBDMirrorCollector.Informer.GetIndexer()),
				namespaces: cephRBDMirrorCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephRBDMirror1,
			},
			wantObjects: []runtime.Object{
				&mockCephRBDMirror1,
			},
		},
		{
			name: "Two CephRBDMirrors exists",
			args: args{
				lister:     cephv1listers.NewCephRBDMirrorLister(cephRBDMirrorCollector.Informer.GetIndexer()),
				namespaces: cephRBDMirrorCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephRBDMirror1,
				&mockCephRBDMirror2,
			},
			wantObjects: []runtime.Object{
				&mockCephRBDMirror1,
				&mockCephRBDMirror2,
			},
		},
	}
	for _, tt := range tests {
		setInformer(t, tt.inputObjects, cephRBDMirrorCollector)
		gotCephRBDMirrors := getAllRBDMirrors(tt.args.lister.(cephv1listers.CephRBDMirrorLister), tt.args.namespaces)
		assert.Len(t, gotCephRBDMirrors, len(tt.wantObjects))
		for _, obj := range gotCephRBDMirrors {
			assert.Contains(t, tt.wantObjects, obj)
		}
		resetInformer(t, tt.inputObjects, cephRBDMirrorCollector)
	}
}

func TestCollectMirrorinDaemonStatus(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephRBDMirrorCollector := getMockCephRBDMirrorCollector(t, mockOpts)

	objReady := mockCephRBDMirror1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.Status{
		Phase: "Ready",
	}

	objCreated := mockCephRBDMirror1.DeepCopy()
	objCreated.Name = objCreated.Name + "created"
	objCreated.Status = &cephv1.Status{
		Phase: "Created",
	}

	objFailed := mockCephRBDMirror1.DeepCopy()
	objFailed.Name = objCreated.Name + "failed"
	objFailed.Status = &cephv1.Status{
		Phase: "Failed",
	}

	tests := Tests{
		{
			name: "Collect CephRBDMirror mirroring daemon status metrics",
			args: args{
				objects: []runtime.Object{
					objReady,
					objCreated,
					objFailed,
				},
			},
		},
		{
			name: "Empty CephRBDMirror",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}
	// daemon status
	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var cephRBDMirrors []*cephv1.CephRBDMirror
			for _, obj := range tt.args.objects {
				cephRBDMirrors = append(cephRBDMirrors, obj.(*cephv1.CephRBDMirror))
			}
			cephRBDMirrorCollector.collectMirrorinDaemonStatus(cephRBDMirrors, ch)
			close(ch)
		}()

		for m := range ch {
			assert.Contains(t, m.Desc().String(), "status")
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)
			labels := metric.GetLabel()
			for _, label := range labels {
				if *label.Name == "name" {
					if *label.Value == objReady.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(0))
					} else if *label.Value == objCreated.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(1))
					} else if *label.Value == objFailed.Name {
						assert.Equal(t, *metric.Gauge.Value, float64(2))
					}
				} else if *label.Name == "namespace" {
					assert.Contains(t, cephRBDMirrorCollector.AllowedNamespaces, *label.Value)
				}
			}
		}
	}

}
