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

var mockCephNFS1 = cephv1.CephNFS{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mockCephNFS-1",
		Namespace: "openshift-storage",
	},
	Status: &cephv1.NFSStatus{},
}

func (c *CephNFSCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockCephNFSCollector(t *testing.T, mockOpts *options.Options) (mockCephNFSCollector *CephNFSCollector) {
	setKubeConfig(t)
	mockCephNFSCollector = NewCephNFSCollector(mockOpts)
	assert.NotNil(t, mockCephNFSCollector)
	return
}

func TestNewCephNFSCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test CephNFSCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockCephNFSCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
	}
}

func TestCollectCephNFSPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephNFSCollector := getMockCephNFSCollector(t, mockOpts)

	objReady := mockCephNFS1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.NFSStatus{Status: cephv1.Status{Phase: "Ready"}}

	objFailed := mockCephNFS1.DeepCopy()
	objFailed.Name = objFailed.Name + "failed"
	objFailed.Status = &cephv1.NFSStatus{Status: cephv1.Status{Phase: "Failed"}}

	objNilStatus := mockCephNFS1.DeepCopy()
	objNilStatus.Name = objNilStatus.Name + "nilstatus"
	objNilStatus.Status = nil

	objUnrecognized := mockCephNFS1.DeepCopy()
	objUnrecognized.Name = objUnrecognized.Name + "unrecognized"
	objUnrecognized.Status = &cephv1.NFSStatus{Status: cephv1.Status{Phase: "SomethingElse"}}

	tests := Tests{
		{
			name: "Collect CephNFS phase metrics",
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
			name: "Empty CephNFS",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var nfses []*cephv1.CephNFS
			for _, obj := range tt.args.objects {
				nfses = append(nfses, obj.(*cephv1.CephNFS))
			}
			cephNFSCollector.collectCephNFSPhase(nfses, ch)
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
					assert.Contains(t, cephNFSCollector.AllowedNamespaces, *label.Value)
				}
			}
			gotCodes[name] = *metric.Gauge.Value
		}

		if len(tt.args.objects) > 0 {
			// one series per instance, not one per phase: nilstatus (empty phase)
			// is actually a valid, known phase; unrecognized is skipped entirely.
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
