package ocs_test

import (
	"context"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	tests "github.com/openshift/ocs-operator/functests"
	k8sbatchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var _ = ginkgo.Describe("job creation", DataValidationTest)

func DataValidationTest() {
	dm := tests.DeployManager
	k8sClient := dm.GetK8sClient()

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentGinkgoTestDescription().Failed {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	ginkgo.Describe("rbd", func() {
		var namespace string
		var pvc *k8sv1.PersistentVolumeClaim
		var job *k8sbatchv1.Job

		ginkgo.BeforeEach(func() {
			namespace = tests.TestNamespace
			pvc = tests.GetRandomPVC(tests.StorageClassRBD, "1Gi")
			job = tests.GetDataValidatorJob(pvc.GetName())
		})

		ginkgo.AfterEach(func() {
			err := k8sClient.BatchV1().Jobs(namespace).Delete(context.TODO(), job.GetName(), metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				gomega.Expect(err).To(gomega.BeNil())
			}
			err = k8sClient.CoreV1().PersistentVolumeClaims(namespace).Delete(context.TODO(), pvc.Name, metav1.DeleteOptions{})
			if err != nil && !errors.IsNotFound(err) {
				gomega.Expect(err).To(gomega.BeNil())
			}
		})
		ginkgo.Context("Create job with pvc", func() {
			ginkgo.It("and verify the data integrity", func() {
				ginkgo.By("Creating PVC")
				err := dm.WaitForPVCBound(pvc, namespace)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Running Job")
				err = dm.WaitForJobSucceeded(job, namespace)
				gomega.Expect(err).To(gomega.BeNil())

				finalJob, err := k8sClient.BatchV1().Jobs(namespace).Get(context.TODO(), job.GetName(), metav1.GetOptions{})
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(finalJob.Status.Succeeded).NotTo(gomega.BeZero())
			})
		})
	})
}
