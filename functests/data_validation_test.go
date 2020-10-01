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
)

var _ = Describe("job creation", DataValidationTest)

func DataValidationTest() {
	deployManager, err := deploymanager.NewDeployManager()
	if err != nil {
		panic("failed to initialize DeployManager")
	}
	k8sClient := deployManager.GetK8sClient()

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
				err := deployManager.WaitForPVCBound(pvc, namespace)
				Expect(err).To(BeNil())

				By("Running Job")
				err = deployManager.WaitForJobSucceeded(job, namespace)
				Expect(err).To(BeNil())

				finalJob, err := k8sClient.BatchV1().Jobs(namespace).Get(job.GetName(), metav1.GetOptions{})
				Expect(err).To(BeNil())
				Expect(finalJob.Status.Succeeded).NotTo(BeZero())
			})
		})
	})
}
