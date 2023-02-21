package ocs_test

import (
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/functests"
)

var _ = ginkgo.XDescribe("Cluster upgrade", ClusterUpgradeTest)

func ClusterUpgradeTest() {

	ginkgo.BeforeEach(func() {
		gomega.RegisterFailHandler(ginkgo.Fail)
		if tests.UpgradeFromOcsCatalogSourceImage == "" {
			ginkgo.Skip("Condition not met for upgrade. Missing OCS CatalogSource image to upgrade from.")
		}
	})

	ginkgo.Describe("ocs", func() {

		ginkgo.BeforeEach(func() {
			ginkgo.By("Preparing for upgrade. Uninstall the current cluster")
			tests.AfterTestSuiteCleanup()
			ginkgo.By("Reinstall a fresh cluster based on upgrade_from image")
			tests.BeforeUpgradeTestSuiteSetup()
		})

		ginkgo.AfterEach(func() {
			if ginkgo.CurrentGinkgoTestDescription().Failed {
				ginkgo.By("Detected upgrade failure. Cleanup the cluster")
				tests.AfterUpgradeTestSuiteCleanup()
				ginkgo.By("Reinstall a fresh cluster")
				tests.BeforeTestSuiteSetup()
			}
		})

		ginkgo.Context("upgrade cluster", func() {
			ginkgo.It("and verify deployment status", func() {
				deployManager := tests.DeployManager

				ginkgo.By("Getting the current csv before the upgrade")
				csv, err := deployManager.GetCsv()
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Upgrading OCS with OLM to the current version from upgrade_from version")
				err = deployManager.UpgradeOCSWithOLM(tests.OcsCatalogSourceImage, tests.OcsSubscriptionChannel)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Waiting for OCS CSV to be posted and installed")
				err = deployManager.WaitForCsvUpgrade(csv.Name, tests.OcsSubscriptionChannel)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Waiting for ocs-operator, rook-ceph-operator and noobaa-operator to come online.")
				err = deployManager.WaitForOCSOperator()
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying ocs-csv has been upgraded to the new version")
				upgradedCsv, err := deployManager.GetCsv()
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(upgradedCsv.Name).ToNot(gomega.Equal(csv.Name))

				ginkgo.By("Verifying operators have been upgraded to the new images in the deployment")
				err = deployManager.VerifyComponentOperators()
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying StorageCluster previously created in the environment is still healthy")
				err = deployManager.WaitOnStorageCluster()
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
	})
}
