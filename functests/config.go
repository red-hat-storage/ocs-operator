package functests

import (
	"flag"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "ocs-functest"

// TestStorageCluster is the name of the storage cluster the test suite installs
const TestStorageCluster = deploymanager.DefaultStorageClusterName

// StorageClassRBD is the name of the ceph rbd storage class the test suite installs
const StorageClassRBD = deploymanager.DefaultStorageClassRBD

var ocsRegistryImage string
var localStorageRegistryImage string

func init() {
	flag.StringVar(&ocsRegistryImage, "ocs-registry-image", "", "The ocs-registry container image to use in the deployment")
	flag.StringVar(&localStorageRegistryImage, "local-storage-registry-image", "", "The local storage registry image to use in the deployment")
}
