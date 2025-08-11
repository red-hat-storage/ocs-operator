package functests

import (
	"flag"
	"fmt"

	deploymanager "github.com/red-hat-storage/ocs-operator/v4/pkg/deploy-manager"
)

// TestNamespace is the namespace we run all the tests in.
const TestNamespace = "ocs-functest"

// TestStorageCluster is the name of the storage cluster the test suite installs
const TestStorageCluster = deploymanager.DefaultStorageClusterName

// StorageClassRBD is the name of the ceph rbd storage class the test suite installs
const StorageClassRBD = deploymanager.DefaultStorageClassRBD

// StorageClassCephFS is the name of the ceph filesystem storage class the test suite installs
const StorageClassCephFS = deploymanager.DefaultStorageClassCephFS

// InstallNamespace is the namespace ocs is installed into
var InstallNamespace string

// OcsSubscriptionChannel is the name of the ocs subscription channel
var OcsSubscriptionChannel string

// UpgradeFromOcsSubscriptionChannel is the name of the ocs subscription channel to upgrade from
var UpgradeFromOcsSubscriptionChannel string

// OcsCatalogSourceImage is the OCS CatalogSource container image to use in the deployment
var OcsCatalogSourceImage string

// UpgradeFromOcsCatalogSourceImage is the OCS CatalogSource container image to upgrade from in the deployment
var UpgradeFromOcsCatalogSourceImage string

// DeployManager is the suite global DeployManager
var DeployManager *deploymanager.DeployManager

// SuiteFailed indicates whether any test in the current suite has failed
var SuiteFailed = false

var ocsClusterUninstall bool
var ocsOperatorInstall bool
var arbiterEnabled bool

func init() {
	flag.StringVar(&OcsCatalogSourceImage, "ocs-catalog-image", "", "The OCS CatalogSource container image to use in the deployment")
	flag.StringVar(&OcsSubscriptionChannel, "ocs-subscription-channel", "", "The subscription channel to reveice updates from")
	flag.StringVar(&InstallNamespace, "install-namespace", "openshift-storage", "The namespace ocs is installed into")
	flag.StringVar(&UpgradeFromOcsCatalogSourceImage, "upgrade-from-ocs-catalog-image", "", "The OCS CatalogSource container image to upgrade from in the deployment")
	flag.StringVar(&UpgradeFromOcsSubscriptionChannel, "upgrade-from-ocs-subscription-channel", "", "The subscription channel to upgrade from")
	flag.BoolVar(&ocsOperatorInstall, "ocs-operator-install", false, "Install the OCS operator before starting tests")
	flag.BoolVar(&ocsClusterUninstall, "ocs-cluster-uninstall", false, "Uninstall the OCS storagecluster and operator after test completion")
	flag.BoolVar(&arbiterEnabled, "arbiter", false, "Deploy the StorageCluster with arbiter enabled")

	flag.Parse()

	dm, err := deploymanager.NewDeployManager()
	if err != nil {
		panic(fmt.Sprintf("failed to initialize DeployManager: %v", err))
	}

	DeployManager = dm
	if arbiterEnabled {
		DeployManager.EnableArbiter()
	}
}
