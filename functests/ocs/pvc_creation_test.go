package ocs_test

import (
	"context"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/functests"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("PVC Creation", PVCCreationTest)

func PVCCreationTest() {
	dm := tests.DeployManager
	k8sClient := dm.GetK8sClient()

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
		})

		ginkgo.AfterEach(func() {
			err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
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
				err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
				gomega.Expect(err).To(gomega.BeNil())
			})
		})
	})
}
