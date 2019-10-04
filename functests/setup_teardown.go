package functests

import (
	"flag"

	"github.com/onsi/gomega"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

// BeforeTestSuiteSetup is the function called to initialize the test environment
func BeforeTestSuiteSetup() {
	flag.Parse()

	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.CreateNamespace(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	err = t.DeployOCSWithOLM(ocsRegistryImage, localStorageRegistryImage)
	gomega.Expect(err).To(gomega.BeNil())

	err = t.StartDefaultStorageCluster()
	gomega.Expect(err).To(gomega.BeNil())

}

// AfterTestSuiteCleanup is the function called to tear down the test environment
func AfterTestSuiteCleanup() {
	flag.Parse()

	t, err := deploymanager.NewDeployManager()
	gomega.Expect(err).To(gomega.BeNil())

	err = t.DeleteNamespaceAndWait(TestNamespace)
	gomega.Expect(err).To(gomega.BeNil())

	// TODO uninstall storage cluster.
	// Right now uninstall doesn't work. Once uninstall functions
	// properly, we'll want to uninstall the storage cluster after
	// the testsuite completes
}
