package deploymanager

import (
	"fmt"
	"os"

	nbv1 "github.com/noobaa/noobaa-operator/v2/pkg/apis/noobaa/v1alpha1"
	olmclient "github.com/operator-framework/operator-lifecycle-manager/pkg/api/client/clientset/versioned"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rookcephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"
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

const minOSDsCount = 3
const minOSDsCountArbiter = 4

func (t *DeployManager) getMinOSDsCount() int {
	if t.ArbiterEnabled() {
		return minOSDsCountArbiter
	}
	return minOSDsCount
}

//nolint:errcheck // ignoring err check as causing failures
func init() {
	ocsv1.SchemeBuilder.AddToScheme(scheme.Scheme)
	rookcephv1.SchemeBuilder.AddToScheme(scheme.Scheme)
	nbv1.SchemeBuilder.AddToScheme(scheme.Scheme)
}

type arbiterConfig struct {
	Enabled bool
	Zone    string
}

// storageClusterConfig stores the configuration of the Storage Cluster that is/will be
// created by the deploy manager.
type storageClusterConfig struct {
	// As deploy manager is mainly used as a replacement for openshift/console,
	// this should capture the options that are presented in the console for OCS
	// installation.
	// For example Encryption, Arbiter etc
	// Eligibility Check:
	// Is this option presented in the console and/or does this option cause a
	// change in the initial Storage Cluster CR provided to the ocs-operator? Yes, it belongs here.

	// Functional tests also use the deploy manager for deployment and if any
	// state needs to be stored for the storageCluster, it belongs here.

	// TODO: Move all the in-built defaults to this struct. These defaults are
	// either hardcoded or are defined as globals in the deploy manager.

	arbiterConf *arbiterConfig
}

// DeployManager is a util tool used by the functional tests
type DeployManager struct {
	olmClient          *olmclient.Clientset
	k8sClient          *kubernetes.Clientset
	rookClient         *rookclient.Clientset
	ocsClient          *rest.RESTClient
	crClient           crclient.Client
	parameterCodec     runtime.ParameterCodec
	storageClusterConf *storageClusterConfig
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
func (t *DeployManager) GetRookClient() *rookclient.Clientset {
	return t.rookClient
}

// GetParameterCodec is the function used to retrieve the parameterCodec
func (t *DeployManager) GetParameterCodec() runtime.ParameterCodec {
	return t.parameterCodec
}

// GetNamespace is the function used to retrieve the installation namespace
func (t *DeployManager) GetNamespace() string {
	return InstallNamespace
}

// NewDeployManager creates a DeployManager struct with default configuration
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
	ocsConfig.GroupVersion = &ocsv1.GroupVersion
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
	rookClient, err := rookclient.NewForConfig(rookConfig)
	if err != nil {
		return nil, err
	}

	// controller-runtime client
	crClient, err := crclient.New(config, crclient.Options{Scheme: scheme.Scheme})
	if err != nil {
		return nil, err
	}

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

	storageClusterConf := getDefaultStorageClusterConfig()

	return &DeployManager{
		olmClient:          olmClient,
		k8sClient:          k8sClient,
		rookClient:         rookClient,
		ocsClient:          ocsClient,
		crClient:           crClient,
		parameterCodec:     parameterCodec,
		storageClusterConf: storageClusterConf,
	}, nil
}

func getDefaultStorageClusterConfig() *storageClusterConfig {
	return &storageClusterConfig{
		arbiterConf: &arbiterConfig{},
	}
}
