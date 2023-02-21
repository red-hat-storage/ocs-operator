package ocs_test

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/functests"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
)

var _ = ginkgo.Describe("PVC Creation", PVCCreationTest)

func PVCCreationTest() {
	dm := tests.DeployManager
	client := dm.Client

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentGinkgoTestDescription().Failed {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	ginkgo.Describe("rbd", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var namespace string

		ginkgo.BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")
			pvc.Namespace = namespace
		})

		ginkgo.AfterEach(func() {
			err := client.Delete(context.TODO(), pvc)
			if err != nil && !errors.IsNotFound(err) {
				gomega.Expect(err).To(gomega.BeNil())
			}
		})

		ginkgo.Context("create pvc", func() {
			ginkgo.It("and verify bound status", func() {
				ginkgo.By("Creating PVC")
				err := dm.WaitForPVCBound(pvc, namespace)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Deleting PVC")
				err = client.Delete(context.TODO(), pvc)
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
	})
}
