package ocs_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/util"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"

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
	deployManager, err := deploymanager.NewDeployManager()
	if err != nil {
		return nil, err
	}
	retSCCObj := &SCCreation{}
	retSCCObj.ocsClient = deployManager.GetOcsClient()
	retSCCObj.parameterCodec = deployManager.GetParameterCodec()
	return retSCCObj, nil
}

func (sccObj *SCCreation) populateDuplicateSC() error {
	defaultStorageCluster, err := deploymanager.DefaultStorageCluster()
	if err != nil {
		return err
	}
	defaultStorageCluster.Name = "duplicate-storagecluster"
	sccObj.duplicateStorageCluster = defaultStorageCluster
	return nil
}

var _ = Describe("StorageCluster Creation", StorageClusterCreationTest)

func StorageClusterCreationTest() {
	var sccObj *SCCreation
	var err error

	BeforeEach(func() {
		RegisterFailHandler(Fail)
		sccObj, err = initSCCreation()
		Expect(err).To(BeNil())
	})

	Describe("Duplicate StorageCluster Creation", func() {
		BeforeEach(func() {
			err := sccObj.populateDuplicateSC()
			Expect(err).To(BeNil())
		})

		AfterEach(func() {
			err := sccObj.ocsClient.Delete().
				Resource("storageclusters").
				Namespace(sccObj.duplicateStorageCluster.Namespace).
				Name(sccObj.duplicateStorageCluster.Name).
				VersionedParams(&metav1.GetOptions{}, sccObj.parameterCodec).
				Do().
				Error()
			Expect(err).To(BeNil())
		})

		Context("create storagecluster", func() {
			It("and verify PhaseIgnored status", func() {
				By("Creating StorageCluster")
				newSc := &ocsv1.StorageCluster{}

				err := sccObj.ocsClient.Post().
					Resource("storageclusters").
					Namespace(sccObj.duplicateStorageCluster.Namespace).
					Name(sccObj.duplicateStorageCluster.Name).
					Body(sccObj.duplicateStorageCluster).
					Do().
					Into(newSc)
				Expect(err).To(BeNil())

				By("Verifying StorageCluster is PhaseIgnored")
				sc := &ocsv1.StorageCluster{}

				Eventually(func() error {
					err = sccObj.ocsClient.Get().
						Resource("storageclusters").
						Namespace(sccObj.duplicateStorageCluster.Namespace).
						Name(sccObj.duplicateStorageCluster.Name).
						VersionedParams(&metav1.GetOptions{}, sccObj.parameterCodec).
						Do().
						Into(sc)
					if err != nil {
						return err
					}
					if sc.Status.Phase == util.PhaseIgnored {
						return nil
					}
					return fmt.Errorf("Waiting on StorageCluster %s/%s to reach Ignored state when it is currently %s", sc.Namespace, sc.Name, sc.Status.Phase)
				}, 10*time.Second, 1*time.Second).ShouldNot(HaveOccurred())
			})
		})
	})
}
