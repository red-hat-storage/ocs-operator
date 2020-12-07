package functests

import (
	"flag"
	"fmt"

	deploymanager "github.com/openshift/ocs-operator/internal/deploy-manager"
)

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "ocs-functest"

// TestStorageCluster is the name of the storage cluster the test suite installs
const TestStorageCluster = deploymanager.DefaultStorageClusterName

// StorageClassRBD is the name of the ceph rbd storage class the test suite installs
const StorageClassRBD = deploymanager.DefaultStorageClassRBD

// OcsSubscriptionChannel is the name of the ocs subscription channel
var OcsSubscriptionChannel string

// UpgradeFromOcsSubscriptionChannel is the name of the ocs subscription channel to upgrade from
var UpgradeFromOcsSubscriptionChannel string

// OcsRegistryImage is the ocs-registry container image to use in the deployment
var OcsRegistryImage string

// UpgradeFromOcsRegistryImage is the ocs-registry container image to upgrade from in the deployment
var UpgradeFromOcsRegistryImage string

// DeployManager is the suite global DeployManager
var DeployManager *deploymanager.DeployManager

// SuiteFailed indicates whether any test in the current suite has failed
var SuiteFailed = false

var ocsClusterUninstall bool

func init() {
	flag.StringVar(&OcsRegistryImage, "ocs-registry-image", "", "The ocs-registry container image to use in the deployment")
	flag.StringVar(&OcsSubscriptionChannel, "ocs-subscription-channel", "", "The subscription channel to reveice updates from")
	flag.StringVar(&UpgradeFromOcsRegistryImage, "upgrade-from-ocs-registry-image", "", "The ocs-registry container image to upgrade from in the deployment")
	flag.StringVar(&UpgradeFromOcsSubscriptionChannel, "upgrade-from-ocs-subscription-channel", "", "The subscription channel to upgrade from")
	flag.BoolVar(&ocsClusterUninstall, "ocs-cluster-uninstall", true, "Uninstall the ocs cluster after tests completion")

	flag.Parse()

	dm, err := deploymanager.NewDeployManager()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize DeployManager: %v", err))
	}
	DeployManager = dm
}
