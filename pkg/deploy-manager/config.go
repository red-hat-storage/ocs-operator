package deploymanager

import (
	"fmt"
	"os"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	olmclient "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	rookv1 "github.com/rook/rook/pkg/client/clientset/versioned"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	crclient "sigs.k8s.io/controller-runtime/pkg/client"
)

// InstallNamespace is the namespace ocs is installed into
const InstallNamespace = "openshift-storage"

// DefaultStorageClusterName is the name of the storage cluster the test suite installs
const DefaultStorageClusterName = "test-storagecluster"

// DefaultStorageClassRBD is the name of the ceph rbd storage class the test suite installs
const DefaultStorageClassRBD = DefaultStorageClusterName + "-ceph-rbd"

// MinOSDsCount represents the minimum number of OSDs required for this testsuite to run.
const MinOSDsCount = 3

func init() {
	ocsv1.SchemeBuilder.AddToScheme(scheme.Scheme)
}

// DeployManager is a util tool used by the functional tests
type DeployManager struct {
	olmClient      *olmclient.Clientset
	k8sClient      *kubernetes.Clientset
	rookClient     *rookv1.Clientset
	ocsClient      *rest.RESTClient
	crClient       crclient.Client
	parameterCodec runtime.ParameterCodec
}

// GetCrClient is the function used to retrieve the controller-runtime client
func (t *DeployManager) GetCrClient() crclient.Client {
	return t.crClient
}

// GetK8sClient is the function used to retrieve the kubernetes client
func (t *DeployManager) GetK8sClient() *kubernetes.Clientset {
	return t.k8sClient
}

// GetOcsClient is the function used to retrieve the ocs client
func (t *DeployManager) GetOcsClient() *rest.RESTClient {
	return t.ocsClient
}

// GetRookClient is the function used to retrieve the rook client
func (t *DeployManager) GetRookClient() *rookv1.Clientset {
	return t.rookClient
}

// GetParameterCodec is the function used to retrieve the parameterCodec
func (t *DeployManager) GetParameterCodec() runtime.ParameterCodec {
	return t.parameterCodec
}

// NewDeployManager is the way to create a DeployManager struct
func NewDeployManager() (*DeployManager, error) {
	codecs := serializer.NewCodecFactory(scheme.Scheme)
	parameterCodec := runtime.NewParameterCodec(scheme.Scheme)

	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("No KUBECONFIG environment variable set")
	}

	// K8s Core api client
	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	config.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	k8sClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, err
	}

	// ocs Operator rest client
	ocsConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	ocsConfig.GroupVersion = &ocsv1.SchemeGroupVersion
	ocsConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: codecs}
	ocsConfig.APIPath = "/apis"
	ocsConfig.ContentType = runtime.ContentTypeJSON
	if ocsConfig.UserAgent == "" {
		ocsConfig.UserAgent = rest.DefaultKubernetesUserAgent()
	}
	ocsClient, err := rest.RESTClientFor(ocsConfig)
	if err != nil {
		return nil, err
	}

	// rook ceph rest client
	rookConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	rookClient, err := rookv1.NewForConfig(rookConfig)
	if err != nil {
		return nil, err
	}

	// controller-runtime client
	crClient, err := crclient.New(config, crclient.Options{Scheme: scheme.Scheme})

	// olm client
	olmConfig, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}
	olmConfig.NegotiatedSerializer = serializer.WithoutConversionCodecFactory{CodecFactory: scheme.Codecs}
	olmConfig.APIPath = "/apis"
	olmConfig.ContentType = runtime.ContentTypeJSON
	olmClient, err := olmclient.NewForConfig(olmConfig)
	if err != nil {
		return nil, err
	}

	return &DeployManager{
		olmClient:      olmClient,
		k8sClient:      k8sClient,
		rookClient:     rookClient,
		ocsClient:      ocsClient,
		crClient:       crClient,
		parameterCodec: parameterCodec,
	}, nil
}
