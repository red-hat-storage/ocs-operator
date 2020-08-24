package collectors

import (
	"reflect"
	"testing"

	"github.com/openshift/ocs-operator/metrics/internal/options"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	cephv1listers "github.com/rook/rook/pkg/client/listers/ceph.rook.io/v1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/clientcmd"
)

var (
	mockOpts = &options.Options{
		Apiserver:         "https://localhost:8443",
		KubeconfigPath:    "",
		Host:              "0.0.0.0",
		Port:              8080,
		ExporterHost:      "0.0.0.0",
		ExporterPort:      8081,
		AllowedNamespaces: []string{"openshift-storage"},
		Help:              false,
	}
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

func setKubeConfig(t *testing.T) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags(mockOpts.Apiserver, mockOpts.KubeconfigPath)
	assert.Nil(t, err, "error: %v", err)

	mockOpts.Kubeconfig = kubeconfig
}

func getMockCephObjectStoreCollector(t *testing.T, mockOpts *options.Options) (mockCephObjectStoreCollector *CephObjectStoreCollector) {
	setKubeConfig(t)
	mockCephObjectStoreCollector = NewCephObjectStoreCollector(mockOpts)
	assert.NotNil(t, mockCephObjectStoreCollector)
	return
}

func setInformerStore(t *testing.T, objs []*cephv1.CephObjectStore, mockCephObjectStoreCollector *CephObjectStoreCollector) {
	if objs != nil {
		for _, obj := range objs {
			err := mockCephObjectStoreCollector.Informer.GetStore().Add(obj)
			assert.Nil(t, err)
		}
	}
}

func resetInformerStore(t *testing.T, objs []*cephv1.CephObjectStore, mockCephObjectStoreCollector *CephObjectStoreCollector) {
	if objs != nil {
		for _, obj := range objs {
			err := mockCephObjectStoreCollector.Informer.GetStore().Delete(obj)
			assert.Nil(t, err)
		}
	}
}

func TestNewCephObjectStoreCollector(t *testing.T) {
	type args struct {
		opts *options.Options
	}
	tests := []struct {
		name string
		args args
	}{
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

	type args struct {
		lister     cephv1listers.CephObjectStoreLister
		namespaces []string
	}
	tests := []struct {
		name                  string
		args                  args
		inputCephObjectStores []*cephv1.CephObjectStore
		wantCephObjectStores  []*cephv1.CephObjectStore
	}{
		{
			name: "CephObjectStore doesn't exist",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputCephObjectStores: []*cephv1.CephObjectStore{},
			// []*cephv1.CephObjectStore(nil) is not DeepEqual to []*cephv1.CephObjectStore{}
			// getAllObjectStores returns []*cephv1.CephObjectStore(nil) if no CephObjectStore is present
			wantCephObjectStores: []*cephv1.CephObjectStore(nil),
		},
		{
			name: "One CephObjectStore exists",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputCephObjectStores: []*cephv1.CephObjectStore{
				&mockCephObjectStore1,
			},
			wantCephObjectStores: []*cephv1.CephObjectStore{
				&mockCephObjectStore1,
			},
		},
		{
			name: "Two CephObjectStores exists",
			args: args{
				lister:     cephv1listers.NewCephObjectStoreLister(cephObjectStoreCollector.Informer.GetIndexer()),
				namespaces: cephObjectStoreCollector.AllowedNamespaces,
			},
			inputCephObjectStores: []*cephv1.CephObjectStore{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
			},
			wantCephObjectStores: []*cephv1.CephObjectStore{
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
			inputCephObjectStores: []*cephv1.CephObjectStore{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
				&mockCephObjectStore3,
			},
			wantCephObjectStores: []*cephv1.CephObjectStore{
				&mockCephObjectStore1,
				&mockCephObjectStore2,
			},
		},
	}
	for _, tt := range tests {
		setInformerStore(t, tt.inputCephObjectStores, cephObjectStoreCollector)
		gotCephObjectStores := getAllObjectStores(tt.args.lister, tt.args.namespaces)
		assert.True(t, reflect.DeepEqual(gotCephObjectStores, tt.wantCephObjectStores))
		resetInformerStore(t, tt.inputCephObjectStores, cephObjectStoreCollector)
	}
}
