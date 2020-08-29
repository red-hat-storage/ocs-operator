package must_gather_test

import (
	"fmt"
	"os"
	"os/exec"
	"testing"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"
)

func TestTests(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "must-gather Test Suite")
}

var _ = BeforeSuite(func() {
	tests.BeforeTestSuiteSetup()
})

var _ = AfterSuite(func() {
	tests.AfterTestSuiteCleanup()
})

var _ = PDescribe("Must Gather", MustGatherTest)

func MustGatherTest() {
	BeforeEach(func() {
		RegisterFailHandler(Fail)
	})

	It("Ensures that a valid cluster dump is collected", func() {
		gopath := os.Getenv("GOPATH")
		Expect(gopath).NotTo(BeEmpty())
		cmd := exec.Command("/bin/bash", gopath+"/src/github.com/openshift/ocs-operator/must-gather/functests/functests.sh")
		output, err := cmd.CombinedOutput()
		fmt.Printf("%s", output)
		Expect(err).To(BeNil())
	})
}
