package functests

import (
	"github.com/onsi/gomega"
)

// BeforeTestSuiteSetup is the function called to initialize the test environment
func BeforeTestSuiteSetup() {

	debug("BeforeTestSuite: deploying OCS\n")
	err := DeployManager.DeployOCSWithOLM(OcsRegistryImage, OcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())

	debug("BeforeTestSuite: starting default StorageCluster\n")
	err = DeployManager.StartDefaultStorageCluster()

	gomega.Expect(err).To(gomega.BeNil())

	debug("BeforeTestSuite: creating Namespace %s\n", TestNamespace)
	err = DeployManager.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	debug("------------------------------\n")
}

// AfterTestSuiteCleanup is the function called to tear down the test environment
func AfterTestSuiteCleanup() {

	debug("\n------------------------------\n")

	// collect debug log before deleting namespace & cluster
	if SuiteFailed {
		debug("AfterTestSuite: collecting debug information\n")
		err := RunMustGather()
		gomega.Expect(err).To(gomega.BeNil())
	}

	debug("AfterTestSuite: deleting Namespace %s\n", TestNamespace)
	err := DeployManager.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	if ocsClusterUninstall {
		debug("AfterTestSuite: uninstalling OCS\n")
		err = DeployManager.UninstallOCS(OcsRegistryImage, OcsSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil(), "error uninstalling OCS: %v", err)
	}
}

// AfterUpgradeTestSuiteCleanup is the function called to tear down the test environment after upgrade failure
func AfterUpgradeTestSuiteCleanup() {

	err := DeployManager.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	// Only called after upgrade failures, so the cluster has to be uninstalled.
	err = DeployManager.UninstallOCS(OcsRegistryImage, OcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())
}

// BeforeUpgradeTestSuiteSetup is the function called to initialize the test environment to the upgrade_from version
func BeforeUpgradeTestSuiteSetup() {

	err := DeployManager.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	err = DeployManager.DeployOCSWithOLM(UpgradeFromOcsRegistryImage, UpgradeFromOcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())

	err = DeployManager.StartDefaultStorageCluster()
	gomega.Expect(err).To(gomega.BeNil())
}
