package functests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ = Describe("PVC Creation", func() {

	BeforeEach(func() {
		RegisterFailHandler(Fail)
	})

	Describe("rbd", func() {
		var testClient *tests.TestClient
		var k8sClient *kubernetes.Clientset
		var pvc *k8sv1.PersistentVolumeClaim
		var namespace string

		BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")

			testClient = tests.NewTestClient()
			k8sClient = testClient.GetK8sClient()
		})

		AfterEach(func() {
			err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
		})

		Context("create pvc", func() {
			It("and verify bound status", func() {
				By("Creating PVC")
				_, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Create(pvc)
				Expect(err).To(BeNil())

				By("Verifying PVC reaches BOUND phase")
				testClient.WaitForPVCBound(pvc.Name, namespace)

				By("Deleting PVC")
				err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{})
				Expect(err).To(BeNil())
			})
		})
	})
})
