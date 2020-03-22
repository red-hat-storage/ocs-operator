package negativetests

import (
	"flag"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "ocs-negativetest"

// TestStorageCluster is the name of the storage cluster the test suite installs
const TestStorageCluster = deploymanager.DefaultStorageClusterName

// OcsSubscriptionChannel is the name of the ocs subscription channel
var OcsSubscriptionChannel string

// OcsRegistryImage is the ocs-registry container image to use in the deployment
var OcsRegistryImage string

var ocsClusterUninstall bool

func init() {
	flag.StringVar(&OcsRegistryImage, "ocs-registry-image", "", "The ocs-registry container image to use in the deployment")
	flag.StringVar(&OcsSubscriptionChannel, "ocs-subscription-channel", "", "The subscription channel to reveice updates from")
	flag.BoolVar(&ocsClusterUninstall, "ocs-cluster-uninstall", true, "Uninstall the ocs cluster after tests completion")
}
