package negativetests_test

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

var _ = Describe("StorageCluster Create Operation Resiliency", func() {
	var (
		ocsClient      *rest.RESTClient
		parameterCodec runtime.ParameterCodec
		d              *deploymanager.DeployManager
	)

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		var err error

		d, err = deploymanager.NewDeployManager()
		Expect(err).To(BeNil())

		ocsClient = d.GetOcsClient()
		parameterCodec = d.GetParameterCodec()
	})

	AfterEach(func() {
		err := ocsClient.Delete().
			Resource("storageclusters").
			Namespace(deploymanager.InstallNamespace).
			Name(deploymanager.DefaultStorageClusterName).
			VersionedParams(&metav1.GetOptions{}, parameterCodec).
			Do().
			Error()

		Expect(err).To(BeNil())
	})

	It("Interrupts and Verifies StorageCluster Creation", func() {
		By("Creating StorageCluster")
		err := d.StartDefaultStorageCluster()
		Expect(err).To(BeNil())

		ocsClient = d.GetOcsClient()
		parameterCodec = d.GetParameterCodec()

		//By("Monitoring for CephCluster object")
		//By("Killing the ocs-operator pod")

		By("Verifying StorageCluster is PhaseReady")
		sc := &ocsv1.StorageCluster{}
		Eventually(func() error {
			err := ocsClient.Get().
				Resource("storageclusters").
				Namespace(deploymanager.InstallNamespace).
				Name(deploymanager.DefaultStorageClusterName).
				VersionedParams(&metav1.GetOptions{}, parameterCodec).
				Do().
				Into(sc)

			if err != nil {
				return err
			}

			if sc.Status.Phase == util.PhaseReady {
				return nil
			}

			return fmt.Errorf("Waiting on StorageCluster %s/%s to reach Ready state when it is currently %s", sc.Namespace, sc.Name, sc.Status.Phase)
		}, 10*time.Second, 10*time.Second).ShouldNot(HaveOccurred())
	})
})
