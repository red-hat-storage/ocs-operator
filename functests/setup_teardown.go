package functests

import (
	"github.com/onsi/gomega"
)

// BeforeTestSuiteSetup is the function called to initialize the test environment
func BeforeTestSuiteSetup() {

	SuiteFailed = true
	debug("BeforeTestSuite: setting the InstallNamespace\n")
	DeployManager.SetNamespace(InstallNamespace)

	if ocsOperatorInstall {
		debug("BeforeTestSuite: deploying OCS Operator\n")
		err := DeployManager.DeployOCSWithOLM(OcsCatalogSourceImage, OcsSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil())
	}

	debug("BeforeTestSuite: starting default StorageCluster\n")
	err := DeployManager.StartDefaultStorageCluster()

	gomega.Expect(err).To(gomega.BeNil())

	debug("BeforeTestSuite: creating Namespace %s\n", TestNamespace)
	err = DeployManager.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	SuiteFailed = false

	debug("------------------------------\n")
}

// AfterTestSuiteCleanup is the function called to tear down the test environment
func AfterTestSuiteCleanup() {

	debug("\n------------------------------\n")

	// collect debug log before deleting namespace & cluster
	debug("AfterTestSuite: collecting debug information\n")
	err := RunMustGather()
	gomega.Expect(err).To(gomega.BeNil())

	debug("AfterTestSuite: deleting Namespace %s\n", TestNamespace)
	err = DeployManager.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	if ocsClusterUninstall {
		debug("AfterTestSuite: deleting default StorageCluster\n")
		err := DeployManager.DeleteStorageCluster()
		gomega.Expect(err).To(gomega.BeNil())

		debug("AfterTestSuite: uninstalling OCS Operator\n")
		err = DeployManager.UninstallOCS(OcsCatalogSourceImage, OcsSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil(), "error uninstalling OCS: %v", err)
	}
}

// AfterUpgradeTestSuiteCleanup is the function called to tear down the test environment after upgrade failure
func AfterUpgradeTestSuiteCleanup() {

	err := DeployManager.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	// Only called after upgrade failures, so the cluster has to be uninstalled.
	err = DeployManager.UninstallOCS(OcsCatalogSourceImage, OcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())
}

// BeforeUpgradeTestSuiteSetup is the function called to initialize the test environment to the upgrade_from version
func BeforeUpgradeTestSuiteSetup() {

	err := DeployManager.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	err = DeployManager.DeployOCSWithOLM(UpgradeFromOcsCatalogSourceImage, UpgradeFromOcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())

	err = DeployManager.StartDefaultStorageCluster()
	gomega.Expect(err).To(gomega.BeNil())
}
