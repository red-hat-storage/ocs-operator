package collectors

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var mockCephRBDMirror1 = cephv1.CephRBDMirror{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mockCephRBDMirror-1",
		Namespace: "openshift-storage",
	},
	Status: &cephv1.RBDMirrorStatus{},
}

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

func TestCollectCephRBDMirrorPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephRBDMirrorCollector := getMockCephRBDMirrorCollector(t, mockOpts)

	objReady := mockCephRBDMirror1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.RBDMirrorStatus{Status: cephv1.Status{Phase: "Ready"}}

	objFailed := mockCephRBDMirror1.DeepCopy()
	objFailed.Name = objFailed.Name + "failed"
	objFailed.Status = &cephv1.RBDMirrorStatus{Status: cephv1.Status{Phase: "Failed"}}

	objNilStatus := mockCephRBDMirror1.DeepCopy()
	objNilStatus.Name = objNilStatus.Name + "nilstatus"
	objNilStatus.Status = nil

	objUnrecognized := mockCephRBDMirror1.DeepCopy()
	objUnrecognized.Name = objUnrecognized.Name + "unrecognized"
	objUnrecognized.Status = &cephv1.RBDMirrorStatus{Status: cephv1.Status{Phase: "SomethingElse"}}

	tests := Tests{
		{
			name: "Collect CephRBDMirror phase metrics",
			args: args{
				objects: []runtime.Object{
					objReady,
					objFailed,
					objNilStatus,
					objUnrecognized,
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

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var mirrors []*cephv1.CephRBDMirror
			for _, obj := range tt.args.objects {
				mirrors = append(mirrors, obj.(*cephv1.CephRBDMirror))
			}
			cephRBDMirrorCollector.collectCephRBDMirrorPhase(mirrors, ch)
			close(ch)
		}()

		gotCodes := map[string]float64{}
		for m := range ch {
			assert.Contains(t, m.Desc().String(), "status_phase")
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)

			var name string
			for _, label := range metric.GetLabel() {
				switch *label.Name {
				case "name":
					name = *label.Value
				case "namespace":
					assert.Contains(t, cephRBDMirrorCollector.AllowedNamespaces, *label.Value)
				}
			}
			gotCodes[name] = *metric.Gauge.Value
		}

		if len(tt.args.objects) > 0 {
			wantReady, _ := rookGenericStatusCode("Ready")
			wantFailed, _ := rookGenericStatusCode("Failed")
			wantEmpty, _ := rookGenericStatusCode("")
			assert.Len(t, gotCodes, 3)
			assert.Equal(t, wantReady, gotCodes[objReady.Name])
			assert.Equal(t, wantFailed, gotCodes[objFailed.Name])
			assert.Equal(t, wantEmpty, gotCodes[objNilStatus.Name])
			_, found := gotCodes[objUnrecognized.Name]
			assert.False(t, found)
		}
	}
}
