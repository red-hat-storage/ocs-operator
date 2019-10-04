package functests_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/util"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/rest"
)

var _ = Describe("StorageCluster Creation", func() {
	var ocsClient *rest.RESTClient
	var parameterCodec runtime.ParameterCodec
	var duplicateStorageCluster *ocsv1.StorageCluster

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())

		ocsClient = deployManager.GetOcsClient()
		parameterCodec = deployManager.GetParameterCodec()
	})

	Describe("Duplicate StorageCluster Creation", func() {

		BeforeEach(func() {
			defaultStorageCluster, err := deploymanager.DefaultStorageCluster()
			Expect(err).To(BeNil())
			defaultStorageCluster.Name = "duplicate-storagecluster"
			duplicateStorageCluster = defaultStorageCluster
		})

		AfterEach(func() {
			err := ocsClient.Delete().
				Resource("storageclusters").
				Namespace(duplicateStorageCluster.Namespace).
				Name(duplicateStorageCluster.Name).
				VersionedParams(&metav1.GetOptions{}, parameterCodec).
				Do().
				Error()
			Expect(err).To(BeNil())
		})

		Context("create storagecluster", func() {
			It("and verify PhaseIgnored status", func() {
				By("Creating StorageCluster")
				newSc := &ocsv1.StorageCluster{}

				err := ocsClient.Post().
					Resource("storageclusters").
					Namespace(duplicateStorageCluster.Namespace).
					Name(duplicateStorageCluster.Name).
					Body(duplicateStorageCluster).
					Do().
					Into(newSc)
				Expect(err).To(BeNil())

				By("Verifying StorageCluster is PhaseIgnored")
				sc := &ocsv1.StorageCluster{}

				gomega.Eventually(func() error {
					err = ocsClient.Get().
						Resource("storageclusters").
						Namespace(duplicateStorageCluster.Namespace).
						Name(duplicateStorageCluster.Name).
						VersionedParams(&metav1.GetOptions{}, parameterCodec).
						Do().
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
})
