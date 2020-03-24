package functests_test

import (
	"flag"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

var _ = PDescribe("Cluster upgrade", func() {
	flag.Parse()

	BeforeEach(func() {
		RegisterFailHandler(Fail)
		if tests.UpgradeFromOcsRegistryImage == "" {
			Skip("Condition not met for upgrade. Missing ocs registry image to upgrade from.")
		}
	})

	Describe("ocs", func() {

		BeforeEach(func() {
			By("Preparing for upgrade. Uninstall the current cluster")
			tests.AfterTestSuiteCleanup()
			By("Reinstall a fresh cluster based on upgrade_from image")
			tests.BeforeUpgradeTestSuiteSetup()
		})

		AfterEach(func() {
			if CurrentGinkgoTestDescription().Failed {
				By("Detected upgrade failure. Cleanup the cluster")
				tests.AfterUpgradeTestSuiteCleanup()
				By("Reinstall a fresh cluster")
				tests.BeforeTestSuiteSetup()
			}
		})

		Context("upgrade cluster", func() {
			It("and verify deployment status", func() {
				deployManager, err := deploymanager.NewDeployManager()
				Expect(err).To(BeNil())

				By("Getting the current csv before the upgrade")
				csv, err := deployManager.GetCsv()
				Expect(err).To(BeNil())

				By("Upgrading OCS with OLM to the current version from upgrade_from version")
				err = deployManager.UpgradeOCSWithOLM(tests.OcsRegistryImage, tests.OcsSubscriptionChannel)
				Expect(err).To(BeNil())

				By("Waiting for OCS CSV to be posted and installed")
				err = deployManager.WaitForCsvUpgrade(csv.Name, tests.OcsSubscriptionChannel)
				Expect(err).To(BeNil())

				By("Waiting for ocs-operator, rook-ceph-operator and noobaa-operator to come online.")
				err = deployManager.WaitForOCSOperator()
				Expect(err).To(BeNil())

				By("Verifying ocs-csv has been upgraded to the new version")
				upgradedCsv, err := deployManager.GetCsv()
				Expect(err).To(BeNil())
				Expect(upgradedCsv.Name).ToNot(Equal(csv.Name))

				By("Verifying operators have been upgraded to the new images in the deployment")
				err = deployManager.VerifyComponentOperators()
				Expect(err).To(BeNil())

				By("Verifying StorageCluster previously created in the environment is still healthy")
				err = deployManager.WaitOnStorageCluster()
				Expect(err).To(BeNil())
			})
		})
	})
})
