package ocs_test

import (
	"context"
	"fmt"
	"time"

	"github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/util"
	tests "github.com/red-hat-storage/ocs-operator/functests"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

type SCCreation struct {
	ocsClient               *rest.RESTClient
	parameterCodec          runtime.ParameterCodec
	duplicateStorageCluster *ocsv1.StorageCluster
}

func initSCCreation() (*SCCreation, error) {
	retSCCObj := &SCCreation{}
	retSCCObj.ocsClient = tests.DeployManager.GetOcsClient()
	retSCCObj.parameterCodec = tests.DeployManager.GetParameterCodec()
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
		if ginkgo.CurrentGinkgoTestDescription().Failed {
			tests.SuiteFailed = tests.SuiteFailed || true
		}
	})

	ginkgo.Describe("Duplicate StorageCluster Creation", func() {
		ginkgo.BeforeEach(func() {
			err := sccObj.populateDuplicateSC()
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.AfterEach(func() {
			err := sccObj.ocsClient.Delete().
				Resource("storageclusters").
				Namespace(sccObj.duplicateStorageCluster.Namespace).
				Name(sccObj.duplicateStorageCluster.Name).
				VersionedParams(&metav1.GetOptions{}, sccObj.parameterCodec).
				Do(context.TODO()).
				Error()
			gomega.Expect(err).To(gomega.BeNil())
		})

		ginkgo.Context("create storagecluster", func() {
			ginkgo.It("and verify PhaseIgnored status", func() {
				ginkgo.By("Creating StorageCluster")
				newSc := &ocsv1.StorageCluster{}

				err := sccObj.ocsClient.Post().
					Resource("storageclusters").
					Namespace(sccObj.duplicateStorageCluster.Namespace).
					Name(sccObj.duplicateStorageCluster.Name).
					Body(sccObj.duplicateStorageCluster).
					Do(context.TODO()).
					Into(newSc)
				gomega.Expect(err).To(gomega.BeNil())

				ginkgo.By("Verifying StorageCluster is PhaseIgnored")
				sc := &ocsv1.StorageCluster{}

				gomega.Eventually(func() error {
					err = sccObj.ocsClient.Get().
						Resource("storageclusters").
						Namespace(sccObj.duplicateStorageCluster.Namespace).
						Name(sccObj.duplicateStorageCluster.Name).
						VersionedParams(&metav1.GetOptions{}, sccObj.parameterCodec).
						Do(context.TODO()).
						Into(sc)
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
