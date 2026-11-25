package collectors

import (
	"testing"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var mockNooBaa1 = nbv1.NooBaa{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mockNooBaa-1",
		Namespace: "openshift-storage",
	},
}

func (c *NooBaaCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockNooBaaCollector(t *testing.T, mockOpts *options.Options) (mockNooBaaCollector *NooBaaCollector) {
	setKubeConfig(t)
	mockNooBaaCollector = NewNooBaaCollector(mockOpts)
	assert.NotNil(t, mockNooBaaCollector)
	return
}

func TestNewNooBaaCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test NooBaaCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockNooBaaCollector(t, tt.args.opts)
		assert.NotNil(t, got.Informer)
	}
}

func TestCollectNooBaaPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	noobaaCollector := getMockNooBaaCollector(t, mockOpts)

	objReady := mockNooBaa1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = nbv1.NooBaaStatus{Phase: nbv1.SystemPhaseReady}

	objRejected := mockNooBaa1.DeepCopy()
	objRejected.Name = objRejected.Name + "rejected"
	objRejected.Status = nbv1.NooBaaStatus{Phase: nbv1.SystemPhaseRejected}

	objUnrecognized := mockNooBaa1.DeepCopy()
	objUnrecognized.Name = objUnrecognized.Name + "unrecognized"
	objUnrecognized.Status = nbv1.NooBaaStatus{Phase: "SomethingElse"}

	tests := Tests{
		{
			name: "Collect NooBaa phase metrics",
			args: args{
				objects: []runtime.Object{
					objReady,
					objRejected,
					objUnrecognized,
				},
			},
		},
		{
			name: "Empty NooBaa",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var noobaas []*nbv1.NooBaa
			for _, obj := range tt.args.objects {
				noobaas = append(noobaas, obj.(*nbv1.NooBaa))
			}
			noobaaCollector.collectNooBaaPhase(noobaas, ch)
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
				if *label.Name == "name" {
					name = *label.Value
				}
			}
			gotCodes[name] = *metric.Gauge.Value
		}

		if len(tt.args.objects) > 0 {
			wantReady, _ := noobaaStatusCode(nbv1.SystemPhaseReady)
			wantRejected, _ := noobaaStatusCode(nbv1.SystemPhaseRejected)
			assert.Len(t, gotCodes, 2)
			assert.Equal(t, wantReady, gotCodes[objReady.Name])
			assert.Equal(t, wantRejected, gotCodes[objRejected.Name])
			_, found := gotCodes[objUnrecognized.Name]
			assert.False(t, found)
		}
	}
}
