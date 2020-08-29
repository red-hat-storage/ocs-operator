package functests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ = Describe("PVC Creation", PVCCreationTest)

func PVCCreationTest() {
	var k8sClient *kubernetes.Clientset
	var deployManager *deploymanager.DeployManager

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())
		k8sClient = deployManager.GetK8sClient()
	})

	Describe("rbd", func() {
		var pvc *k8sv1.PersistentVolumeClaim
		var namespace string

		BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")
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
				deployManager.WaitForPVCBound(pvc.Name, namespace)

				By("Deleting PVC")
				err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{})
				Expect(err).To(BeNil())
			})
		})
	})
}
