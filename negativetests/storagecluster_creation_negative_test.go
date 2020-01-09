package negativetests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
)

var _ = Describe("StorageCluster Creation Negative Scenarios", func() {
	//var ocsClient *rest.RESTClient
	//var parameterCodec runtime.ParameterCodec

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		_, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())

		//ocsClient = deployManager.GetOcsClient()
		//parameterCodec = deployManager.GetParameterCodec()
	})

	Describe("Default StorageCluster Object", func() {
		_, err := deploymanager.DefaultStorageCluster()
		Expect(err).To(BeNil())
	})
})
