package collectors

import (
	"testing"

	"github.com/openshift/ocs-operator/metrics/internal/options"
	"github.com/stretchr/testify/assert"
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
