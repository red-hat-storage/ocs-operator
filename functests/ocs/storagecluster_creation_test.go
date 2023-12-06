package ocs_test

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	tests "github.com/red-hat-storage/ocs-operator/v4/functests"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

type SCCreation struct {
	client                  client.Client
	duplicateStorageCluster *ocsv1.StorageCluster
}

func initSCCreation() (*SCCreation, error) {
	retSCCObj := &SCCreation{}
	retSCCObj.client = tests.DeployManager.Client
	return retSCCObj, nil
}

func (sccObj *SCCreation) populateDuplicateSC() error {
	defaultStorageCluster, err := tests.DeployManager.DefaultStorageCluster()
	if err != nil {
		return err
	}
	defaultStorageCluster.Name = "duplicate-storagecluster"
	sccObj.duplicateStorageCluster = defaultStorageCluster
	return nil
}

var _ = ginkgo.Describe("StorageCluster Creation", StorageClusterCreationTest)

func StorageClusterCreationTest() {
	var sccObj *SCCreation
	var err error

	ginkgo.BeforeEach(func() {
		sccObj, err = initSCCreation()
		gomega.Expect(err).To(gomega.BeNil())
	})

	ginkgo.AfterEach(func() {
		if ginkgo.CurrentSpecReport().Failed() {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	ginkgo.Describe("Duplicate StorageCluster Creation", func() {
		ginkgo.BeforeEach(func() {
			err := sccObj.populateDuplicateSC()
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.AfterEach(func() {
			err := sccObj.client.Delete(context.TODO(), sccObj.duplicateStorageCluster)
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.Context("create storagecluster", func() {
			ginkgo.It("and verify PhaseIgnored status", func() {
				ginkgo.By("Creating StorageCluster")
				err := sccObj.client.Create(context.TODO(), sccObj.duplicateStorageCluster)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying StorageCluster is PhaseIgnored")
				sc := &ocsv1.StorageCluster{}

				gomega.Eventually(func() error {
					err = sccObj.client.Get(context.TODO(), client.ObjectKey{
						Name:      sccObj.duplicateStorageCluster.Name,
						Namespace: sccObj.duplicateStorageCluster.Namespace,
					}, sc)
					if err != nil {
						return err
					}
					if sc.Status.Phase == util.PhaseIgnored {
						return nil
					}
					return fmt.Errorf("Waiting on StorageCluster %s/%s to reach Ignored state when it is currently %s", sc.Namespace, sc.Name, sc.Status.Phase)
				}, 10*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
			})
		})
	})
}
