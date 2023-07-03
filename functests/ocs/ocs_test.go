package ocs_test

import (
	"testing"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/v4/functests"
)

func TestTests(t *testing.T) {
	gomega.RegisterFailHandler(ginkgo.Fail)
	ginkgo.RunSpecs(t, "OCS Test Suite")
}

var _ = ginkgo.BeforeSuite(func() {
	tests.BeforeTestSuiteSetup()
})

var _ = ginkgo.AfterSuite(func() {
	tests.AfterTestSuiteCleanup()
})
