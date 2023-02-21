package ocs_test

import (
	"context"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	tests "github.com/red-hat-storage/ocs-operator/functests"
	k8sbatchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
)

var _ = ginkgo.Describe("job creation", DataValidationTest)

func DataValidationTest() {
	dm := tests.DeployManager
	client := dm.Client

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
			pvc.Namespace = namespace
			job = tests.GetDataValidatorJob(pvc.GetName())
			job.Namespace = namespace
		})

		ginkgo.AfterEach(func() {
			err := client.Delete(context.TODO(), job)
			if err != nil && !errors.IsNotFound(err) {
				gomega.Expect(err).To(gomega.BeNil())
			}
			err = client.Delete(context.TODO(), pvc)
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
				finalJob := &k8sbatchv1.Job{}
				err = client.Get(context.TODO(), types.NamespacedName{
					Name:      job.GetName(),
					Namespace: namespace,
				}, finalJob)
				gomega.Expect(err).To(gomega.BeNil())
				gomega.Expect(finalJob.Status.Succeeded).NotTo(gomega.BeZero())
			})
		})
	})
}
