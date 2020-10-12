package ocs_test

import (
	"context"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = Describe("PVC Creation", PVCCreationTest)

func PVCCreationTest() {
	dm := tests.DeployManager
	k8sClient := dm.GetK8sClient()

	AfterEach(func() {
		if CurrentGinkgoTestDescription().Failed {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	Describe("rbd", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var namespace string

		BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")
		})

		AfterEach(func() {
			err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
		})

		Context("create pvc", func() {
			It("and verify bound status", func() {
				By("Creating PVC")
				err := dm.WaitForPVCBound(pvc, namespace)
				Expect(err).To(BeNil())

				By("Deleting PVC")
				err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
				Expect(err).To(BeNil())
			})
		})
	})
}
