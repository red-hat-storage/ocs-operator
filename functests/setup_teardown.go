package functests

import (
	"flag"
	"os"
	"os/exec"

	"github.com/onsi/gomega"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

// BeforeTestSuiteSetup is the function called to initialize the test environment
func BeforeTestSuiteSetup() {
	flag.Parse()

	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.DeployOCSWithOLM(OcsRegistryImage, OcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())

	err = t.StartDefaultStorageCluster()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())
}

// AfterTestSuiteCleanup is the function called to tear down the test environment
func AfterTestSuiteCleanup() {
	flag.Parse()

	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	// collect debug log before deleting namespace & cluster
	gopath := os.Getenv("GOPATH")
	cmd := exec.Command("/bin/bash", gopath+"/src/github.com/openshift/ocs-operator/hack/dump-debug-info.sh")
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	gomega.Expect(err).To(gomega.BeNil(), "Error dumping debug info: %v", output)

	err = t.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	if ocsClusterUninstall {
		err = t.UninstallOCS(OcsRegistryImage, OcsSubscriptionChannel)
		gomega.Expect(err).To(gomega.BeNil())
	}
}

// AfterUpgradeTestSuiteCleanup is the function called to tear down the test environment after upgrade failure
func AfterUpgradeTestSuiteCleanup() {
	flag.Parse()
	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	// Only called after upgrade failures, so the cluster has to be uninstalled.
	err = t.UninstallOCS(OcsRegistryImage, OcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())
}

// BeforeUpgradeTestSuiteSetup is the function called to initialize the test environment to the upgrade_from version
func BeforeUpgradeTestSuiteSetup() {
	flag.Parse()

	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	err = t.DeployOCSWithOLM(UpgradeFromOcsRegistryImage, UpgradeFromOcsSubscriptionChannel)
	gomega.Expect(err).To(gomega.BeNil())

	err = t.StartDefaultStorageCluster()
	gomega.Expect(err).To(gomega.BeNil())
}
