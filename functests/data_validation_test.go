package functests_test

import (
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	tests "github.com/openshift/ocs-operator/functests"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	k8sbatchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ = Describe("job creation", func() {
	var k8sClient *kubernetes.Clientset

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())

		k8sClient = deployManager.GetK8sClient()
	})

	Describe("rbd", func() {
		var namespace string
		var pvc *k8sv1.PersistentVolumeClaim
		var job *k8sbatchv1.Job

		BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")
			job = tests.GetDataValidatorJob(pvc.GetName())
		})

		AfterEach(func() {
			err := k8sClient.BatchV1().Jobs(namespace).Delete(job.GetName(), &metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
			err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(pvc.Name, &metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				Expect(err).To(BeNil())
			}
		})
		Context("Create job with pvc", func() {
			It("and verify the data integrity", func() {
				By("Creating PVC")
				_, err := k8sClient.CoreV1().PersistentVolumeClaims(namespace).Create(pvc)
				Expect(err).To(BeNil())

				By("Verifying PVC reaches BOUND phase")
				tests.WaitForPVCBound(k8sClient, pvc.Name, namespace)

				By("Creating job")
				_, err = k8sClient.BatchV1().Jobs(namespace).Create(job)
				Expect(err).To(BeNil())

				By("Verifying job succeeds in data validation")
				tests.WaitForJobSucceeded(k8sClient, job.GetName(), namespace)

				finalJob, err := k8sClient.BatchV1().Jobs(namespace).Get(job.GetName(), metav1.GetOptions{})
				Expect(err).To(BeNil())
				Expect(finalJob.Status.Succeeded).NotTo(BeZero())
			})
		})
	})
})
