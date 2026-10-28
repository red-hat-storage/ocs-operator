package collectors

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
)

var (
	mockCephFilesystem1 = cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephFilesystem-1",
			Namespace: "openshift-storage",
		},
		Status: &cephv1.CephFilesystemStatus{},
	}
	mockCephFilesystem2 = cephv1.CephFilesystem{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephFilesystem-2",
			Namespace: "openshift-storage",
		},
		Status: &cephv1.CephFilesystemStatus{},
	}
	mockCephFilesystemSubVolumeGroup1 = cephv1.CephFilesystemSubVolumeGroup{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mockCephFilesystemSubVolumeGroup",
			Namespace: "openshift-storage",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind: "StorageConsumer",
					Name: "consumer-a",
				},
			},
		},
		Spec: cephv1.CephFilesystemSubVolumeGroupSpec{
			FilesystemName: "mockCephFilesystem-1",
		},
		Status: &cephv1.CephFilesystemSubVolumeGroupStatus{},
	}
)

func (c *CephFilesystemCollector) GetInformer() cache.SharedIndexInformer {
	return c.Informer
}

func getMockCephFilesystemCollector(t *testing.T, mockOpts *options.Options) (mockCephFilesystemCollector *CephFilesystemCollector) {
	setKubeConfig(t)
	mockCephFilesystemCollector = NewCephFilesystemCollector(mockOpts)
	assert.NotNil(t, mockCephFilesystemCollector)
	return
}

func TestNewCephFilesystemCollector(t *testing.T) {
	tests := Tests{
		{
			name: "Test CephFilesystemCollector",
			args: args{
				opts: mockOpts,
			},
		},
	}
	for _, tt := range tests {
		got := getMockCephFilesystemCollector(t, tt.args.opts)
		assert.NotNil(t, got.AllowedNamespaces)
		assert.NotNil(t, got.Informer)
		assert.NotNil(t, got.InformerSvg)
	}
}

func TestGetAllCephFilesystems(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephFilesystemCollector := getMockCephFilesystemCollector(t, mockOpts)

	tests := Tests{
		{
			name: "CephFilesystems doesn't exist",
			args: args{
				lister:     cephv1listers.NewCephFilesystemLister(cephFilesystemCollector.Informer.GetIndexer()),
				namespaces: cephFilesystemCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{},
			wantObjects:  []runtime.Object(nil),
		},
		{
			name: "Two CephFilesystems exist",
			args: args{
				lister:     cephv1listers.NewCephFilesystemLister(cephFilesystemCollector.Informer.GetIndexer()),
				namespaces: cephFilesystemCollector.AllowedNamespaces,
			},
			inputObjects: []runtime.Object{
				&mockCephFilesystem1,
				&mockCephFilesystem2,
			},
			wantObjects: []runtime.Object{
				&mockCephFilesystem1,
				&mockCephFilesystem2,
			},
		},
	}
	for _, tt := range tests {
		setInformer(t, tt.inputObjects, cephFilesystemCollector)
		gotCephFilesystems := getAllCephFilesystems(tt.args.lister.(cephv1listers.CephFilesystemLister), tt.args.namespaces)
		assert.Len(t, gotCephFilesystems, len(tt.wantObjects))
		for _, obj := range gotCephFilesystems {
			assert.Contains(t, tt.wantObjects, obj)
		}
		resetInformer(t, tt.inputObjects, cephFilesystemCollector)
	}
}

func TestCollectCephFilesystemPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephFilesystemCollector := getMockCephFilesystemCollector(t, mockOpts)

	objReady := mockCephFilesystem1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.CephFilesystemStatus{Phase: cephv1.ConditionReady}

	objFailure := mockCephFilesystem1.DeepCopy()
	objFailure.Name = objFailure.Name + "failure"
	objFailure.Status = &cephv1.CephFilesystemStatus{Phase: cephv1.ConditionFailure}

	objNilStatus := mockCephFilesystem1.DeepCopy()
	objNilStatus.Name = objNilStatus.Name + "nilstatus"
	objNilStatus.Status = nil

	tests := Tests{
		{
			name: "Collect CephFilesystem phase metrics",
			args: args{
				objects: []runtime.Object{
					objReady,
					objFailure,
					objNilStatus,
				},
			},
		},
		{
			name: "Empty CephFilesystem",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var filesystems []*cephv1.CephFilesystem
			for _, obj := range tt.args.objects {
				filesystems = append(filesystems, obj.(*cephv1.CephFilesystem))
			}
			cephFilesystemCollector.collectCephFilesystemPhase(filesystems, ch)
			close(ch)
		}()

		gotPhases := map[string]map[string]float64{}
		for m := range ch {
			assert.Contains(t, m.Desc().String(), "status_phase")
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)

			var name, phase string
			for _, label := range metric.GetLabel() {
				switch *label.Name {
				case "name":
					name = *label.Value
				case "namespace":
					assert.Contains(t, cephFilesystemCollector.AllowedNamespaces, *label.Value)
				case "phase":
					phase = *label.Value
				}
			}
			if gotPhases[name] == nil {
				gotPhases[name] = map[string]float64{}
			}
			gotPhases[name][phase] = *metric.Gauge.Value
		}

		if len(tt.args.objects) > 0 {
			assert.Len(t, gotPhases[objReady.Name], len(cephFilesystemPhases))
			assert.Equal(t, float64(1), gotPhases[objReady.Name]["Ready"])
			assert.Equal(t, float64(0), gotPhases[objReady.Name]["Failure"])

			assert.Equal(t, float64(1), gotPhases[objFailure.Name]["Failure"])
			assert.Equal(t, float64(0), gotPhases[objFailure.Name]["Ready"])

			for _, phase := range cephFilesystemPhases {
				assert.Equal(t, float64(0), gotPhases[objNilStatus.Name][string(phase)])
			}
		}
	}
}

func TestCollectCephFilesystemSubVolumeGroupPhase(t *testing.T) {
	mockOpts.StopCh = make(chan struct{})
	defer close(mockOpts.StopCh)

	cephFilesystemCollector := getMockCephFilesystemCollector(t, mockOpts)

	objReady := mockCephFilesystemSubVolumeGroup1.DeepCopy()
	objReady.Name = objReady.Name + "ready"
	objReady.Status = &cephv1.CephFilesystemSubVolumeGroupStatus{Phase: cephv1.ConditionReady}

	objFailure := mockCephFilesystemSubVolumeGroup1.DeepCopy()
	objFailure.Name = objFailure.Name + "failure"
	objFailure.Status = &cephv1.CephFilesystemSubVolumeGroupStatus{Phase: cephv1.ConditionFailure}

	objNilStatus := mockCephFilesystemSubVolumeGroup1.DeepCopy()
	objNilStatus.Name = objNilStatus.Name + "nilstatus"
	objNilStatus.Status = nil

	tests := Tests{
		{
			name: "Collect CephFilesystemSubVolumeGroup phase metrics",
			args: args{
				objects: []runtime.Object{
					objReady,
					objFailure,
					objNilStatus,
				},
			},
		},
		{
			name: "Empty CephFilesystemSubVolumeGroup",
			args: args{
				objects: []runtime.Object{},
			},
		},
	}

	for _, tt := range tests {
		ch := make(chan prometheus.Metric)
		metric := dto.Metric{}
		go func() {
			var svgs []*cephv1.CephFilesystemSubVolumeGroup
			for _, obj := range tt.args.objects {
				svgs = append(svgs, obj.(*cephv1.CephFilesystemSubVolumeGroup))
			}
			cephFilesystemCollector.collectCephFilesystemSubVolumeGroupPhase(svgs, ch)
			close(ch)
		}()

		gotCodes := map[string]float64{}
		for m := range ch {
			assert.Contains(t, m.Desc().String(), "status_phase")
			metric.Reset()
			err := m.Write(&metric)
			assert.Nil(t, err)

			var svgName string
			for _, label := range metric.GetLabel() {
				switch *label.Name {
				case "subvolume_group":
					svgName = *label.Value
				case "namespace":
					assert.Contains(t, cephFilesystemCollector.AllowedNamespaces, *label.Value)
				case "name":
					assert.Equal(t, objReady.Spec.FilesystemName, *label.Value)
				case "consumer_name":
					assert.Equal(t, "consumer-a", *label.Value)
				}
			}
			gotCodes[svgName] = *metric.Gauge.Value
		}

		if len(tt.args.objects) > 0 {
			// one series per instance, not one per phase: nilstatus has an
			// unrecognized ("") phase and is skipped entirely.
			assert.Len(t, gotCodes, 2)
			assert.Equal(t, float64(3), gotCodes[objReady.Name])
			assert.Equal(t, float64(4), gotCodes[objFailure.Name])
			_, found := gotCodes[objNilStatus.Name]
			assert.False(t, found)
		}
	}
}
