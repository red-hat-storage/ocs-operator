package collectors

import (
	"context"
	"testing"

	"net/http"

	libbucket "github.com/kube-object-storage/lib-bucket-provisioner/pkg/apis/objectbucket.io/v1alpha1"
	bktclient "github.com/kube-object-storage/lib-bucket-provisioner/pkg/client/clientset/versioned"
	"github.com/red-hat-storage/ocs-operator/metrics/v4/internal/options"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/cache"
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
)

type args struct {
	objects    []runtime.Object
	namespaces []string
	opts       *options.Options
	lister     interface{}
}

type Tests = []struct {
	name         string
	args         args
	inputObjects []runtime.Object
	wantObjects  []runtime.Object
}

type Informer interface {
	GetInformer() cache.SharedIndexInformer
}

func setKubeConfig(t *testing.T) {
	kubeconfig, err := clientcmd.BuildConfigFromFlags(mockOpts.Apiserver, mockOpts.KubeconfigPath)
	assert.Nil(t, err, "error: %v", err)

	mockOpts.Kubeconfig = kubeconfig
}

func setInformer(t *testing.T, objs []runtime.Object, informer Informer) {
	for _, obj := range objs {
		err := informer.GetInformer().GetStore().Add(obj)
		assert.Nil(t, err)
	}
}

func resetInformer(t *testing.T, objs []runtime.Object, informer Informer) {
	for _, obj := range objs {
		err := informer.GetInformer().GetStore().Delete(obj)
		assert.Nil(t, err)
	}
}

func createOBs(t *testing.T, objs []runtime.Object, bktclient bktclient.Interface) {
	for _, obj := range objs {
		_, err := bktclient.ObjectbucketV1alpha1().ObjectBuckets().Create(context.TODO(), obj.(*libbucket.ObjectBucket), metav1.CreateOptions{})
		assert.NoError(t, err)
	}
}

func deleteOBs(t *testing.T, objs []runtime.Object, bktclient bktclient.Interface) {
	for _, obj := range objs {
		err := bktclient.ObjectbucketV1alpha1().ObjectBuckets().Delete(context.TODO(), obj.(*libbucket.ObjectBucket).Name, metav1.DeleteOptions{})
		assert.NoError(t, err)
	}
}

// MockClient is the mock of the HTTP Client
// It can be used to mock HTTP request/response from the rgw admin ops API
type MockClient struct {
	// MockDo is a type that mock the Do method from the HTTP package
	MockDo MockDoType
}

// MockDoType is a custom type that allows setting the function that our Mock Do func will run instead
type MockDoType func(req *http.Request) (*http.Response, error)

// Do is the mock client's `Do` func
func (m *MockClient) Do(req *http.Request) (*http.Response, error) { return m.MockDo(req) }
