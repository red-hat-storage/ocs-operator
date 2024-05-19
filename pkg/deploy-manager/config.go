package deploymanager

import (
	"fmt"
	"os"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	operatorv1 "github.com/operator-framework/api/pkg/operators/v1"
	operatorv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	rookcephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	k8sscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/clientcmd"
	client "sigs.k8s.io/controller-runtime/pkg/client"
)

// InstallNamespace is the namespace ocs is installed into
var InstallNamespace string

// DefaultStorageClusterName is the name of the storage cluster the test suite installs
const DefaultStorageClusterName = "test-storagecluster"

// DefaultStorageClusterStorageSystemName is the name of the storage system owned by default storage cluster
const DefaultStorageClusterStorageSystemName = "test-storage-system"

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

var scheme = runtime.NewScheme()

func init() {
	utilruntime.Must(k8sscheme.AddToScheme(scheme))
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(batchv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1.AddToScheme(scheme))
	utilruntime.Must(operatorv1alpha1.AddToScheme(scheme))
	utilruntime.Must(ocsv1.AddToScheme(scheme))
	utilruntime.Must(rookcephv1.AddToScheme(scheme))
	utilruntime.Must(nbv1.AddToScheme(scheme))
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
	Client             client.Client
	storageClusterConf *storageClusterConfig
}

// GetNamespace is the function used to retrieve the installation namespace
func (t *DeployManager) GetNamespace() string {
	return InstallNamespace
}

// SetNamespace is the function used to set the installation namespace
func (t *DeployManager) SetNamespace(namespace string) {
	InstallNamespace = namespace
}

// NewDeployManager creates a DeployManager struct with default configuration
func NewDeployManager() (*DeployManager, error) {
	kubeconfig := os.Getenv("KUBECONFIG")
	if kubeconfig == "" {
		return nil, fmt.Errorf("No KUBECONFIG environment variable set")
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		return nil, err
	}

	client, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	storageClusterConf := getDefaultStorageClusterConfig()

	return &DeployManager{
		Client:             client,
		storageClusterConf: storageClusterConf,
	}, nil
}

func getDefaultStorageClusterConfig() *storageClusterConfig {
	return &storageClusterConfig{
		arbiterConf: &arbiterConfig{},
	}
}
