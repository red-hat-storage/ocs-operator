package functests_test

import (
	"fmt"
	"os"
	"os/exec"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
)

var _ = Describe("Must Gather", func() {
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
})
