package functests

import (
	"os"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

// InstallNamespace is the namespace ocs is installed into
const InstallNamespace = "openshift-storage"

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "ocs-functest"

// TestStorageCluster is the name of the storage cluster the test suite installs
const TestStorageCluster = "test-storagecluster"

// StorageClassRBD is the name of the ceph rbd storage class the test suite installs
const StorageClassRBD = TestStorageCluster + "-ceph-rbd"

// MinOSDsCount represents the minimum number of OSDs required for this testsuite to run.
const MinOSDsCount = 3

var namespaces = []string{InstallNamespace, TestNamespace}

func init() {
	ocsv1.SchemeBuilder.AddToScheme(scheme.Scheme)
}

// ShouldSkip will skip the functional test if the suite isn't invoked by our make target
// This prevents the unit tests from failing when `go test -v ./...` is invoked
func ShouldSkip() {
	defer ginkgo.GinkgoRecover()
	if !shouldExecute() {
		ginkgo.Skip("Only executed as part of 'make functest'")
	}
}

func shouldExecute() bool {
	// We use this to keep the functional tests from failing
	// when the unit tests are being called.
	shouldExecute := os.Getenv("OCS_EXECUTE_FUNC_TESTS")
	if shouldExecute == "" {
		return false
	}
	return true

}

// GetK8sClient is the function used to retrieve the kubernetes client
func (t *TestClient) GetK8sClient() *kubernetes.Clientset {
	return t.k8sClient
}

// TestClient is a util tool used by the functional tests
type TestClient struct {
	k8sClient      *kubernetes.Clientset
	restClient     *rest.RESTClient
	parameterCodec runtime.ParameterCodec
}

// NewTestClient is the way to create a TestClient struct
func NewTestClient() *TestClient {
	codecs := serializer.NewCodecFactory(scheme.Scheme)
	parameterCodec := runtime.NewParameterCodec(scheme.Scheme)

	kubeconfig := os.Getenv("KUBECONFIG")
	gomega.Expect(kubeconfig).ToNot(gomega.Equal(""))

	// K8s Core api client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).To(gomega.BeNil())
	config.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: scheme.Codecs}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	k8sClient, err := kubernetes.NewForConfig(config)
	gomega.Expect(err).To(gomega.BeNil())

	// ocs Operator rest client
	ocsConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	gomega.Expect(err).To(gomega.BeNil())
	ocsConfig.GroupVersion = &ocsv1.SchemeGroupVersion
	ocsConfig.NegotiatedSerializer = serializer.DirectCodecFactory{CodecFactory: codecs}
	ocsConfig.APIPath = "/apis"
	ocsConfig.ContentType = runtime.ContentTypeJSON
	if ocsConfig.UserAgent == "" {
		ocsConfig.UserAgent = restclient.DefaultKubernetesUserAgent()
	}
	restClient, err := rest.RESTClientFor(ocsConfig)
	gomega.Expect(err).To(gomega.BeNil())

	return &TestClient{
		k8sClient:      k8sClient,
		restClient:     restClient,
		parameterCodec: parameterCodec,
	}
}
