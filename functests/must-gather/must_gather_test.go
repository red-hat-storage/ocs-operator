package must_gather_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	tests "github.com/red-hat-storage/ocs-operator/functests"
)

func TestTests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "must-gather Test Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	tests.BeforeTestSuiteSetup()
})

var _ = ginkgo.AfterSuite(func() {
	tests.AfterTestSuiteCleanup()
})

var _ = ginkgo.XDescribe("Must Gather", MustGatherTest)

func MustGatherTest() {
	ginkgo.It("Ensures that a valid cluster dump is collected", func() {
		ginkgo.By("Running oc adm must-gather")
		err := tests.RunMustGather()
		gomega.Expect(err).To(gomega.BeNil())
	})
}
