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

var mockCephNVMeOFGateway1 = cephv1.CephNVMeOFGateway{
	ObjectMeta: metav1.ObjectMeta{
		Name:      "mockCephNVMeOFGateway-1",
		Namespace: "openshift-storage",
	},
	Status: &cephv1.NVMeOFGatewayStatus{},
}

func (c *CephNVMeOFGatewayCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockCephNVMeOFGatewayCollector(t *testing.T, mockOpts *options.Options) (mockCephNVMeOFGatewayCollector *CephNVMeOFGatewayCollector) {
	setKubeConfig(t)
	mockCephNVMeOFGatewayCollector = NewCephNVMeOFGatewayCollector(mockOpts)
	assert.NotNil(t, mockCephNVMeOFGatewayCollector)
	return
}

func TestNewCephNVMeOFGatewayCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test CephNVMeOFGatewayCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockCephNVMeOFGatewayCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
	}
}

func TestCollectCephNVMeOFGatewayPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephNVMeOFGatewayCollector := getMockCephNVMeOFGatewayCollector(t, mockOpts)

	objReady := mockCephNVMeOFGateway1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.NVMeOFGatewayStatus{Status: cephv1.Status{Phase: "Ready"}}

	objFailed := mockCephNVMeOFGateway1.DeepCopy()
	objFailed.Name = objFailed.Name + "failed"
	objFailed.Status = &cephv1.NVMeOFGatewayStatus{Status: cephv1.Status{Phase: "Failed"}}

	objNilStatus := mockCephNVMeOFGateway1.DeepCopy()
	objNilStatus.Name = objNilStatus.Name + "nilstatus"
	objNilStatus.Status = nil

	objUnrecognized := mockCephNVMeOFGateway1.DeepCopy()
	objUnrecognized.Name = objUnrecognized.Name + "unrecognized"
	objUnrecognized.Status = &cephv1.NVMeOFGatewayStatus{Status: cephv1.Status{Phase: "SomethingElse"}}

	tests := Tests{
		{
			name: "Collect CephNVMeOFGateway phase metrics",
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
			name: "Empty CephNVMeOFGateway",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var gateways []*cephv1.CephNVMeOFGateway
			for _, obj := range tt.args.objects {
				gateways = append(gateways, obj.(*cephv1.CephNVMeOFGateway))
			}
			cephNVMeOFGatewayCollector.collectCephNVMeOFGatewayPhase(gateways, ch)
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
					assert.Contains(t, cephNVMeOFGatewayCollector.AllowedNamespaces, *label.Value)
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
