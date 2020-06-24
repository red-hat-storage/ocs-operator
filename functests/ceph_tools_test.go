package functests_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	k8sv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

var _ = Describe("Rook Ceph Tools", func() {
	var k8sClient *kubernetes.Clientset
	var ocsClient *rest.RESTClient
	var parameterCodec runtime.ParameterCodec

	disableToolsPatch := `[{ "op": "replace", "path": "/spec/enableCephTools", "value": false }]`
	enableToolsPatch := `[{ "op": "replace", "path": "/spec/enableCephTools", "value": true }]`

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil())
		k8sClient = deployManager.GetK8sClient()
		ocsClient = deployManager.GetOcsClient()
		parameterCodec = deployManager.GetParameterCodec()
	})

	patchOcsInit := func(patch string) {

		init := &ocsv1.OCSInitialization{}
		err := ocsClient.Patch(types.JSONPatchType).
			Resource("ocsinitializations").
			Namespace(deploymanager.InstallNamespace).
			Name("ocsinit").
			Body([]byte(patch)).
			VersionedParams(&metav1.GetOptions{}, parameterCodec).
			Do().
			Into(init)

		Expect(err).To(BeNil())
	}

	Describe("Deployment", func() {
		AfterEach(func() {
			patchOcsInit(disableToolsPatch)
		})

		It("Ensure enable tools works", func() {

			By("Setting enableCephTools=true")
			patchOcsInit(enableToolsPatch)

			By("Ensuring tools are created")
			Eventually(func() error {
				pods, err := k8sClient.CoreV1().Pods(deploymanager.InstallNamespace).List(metav1.ListOptions{LabelSelector: "app=rook-ceph-tools"})
				if err != nil {
					return err
				}

				if len(pods.Items) == 0 {
					return fmt.Errorf("waiting on a rook-tools-pod to come online")
				}

				if pods.Items[0].Status.Phase != k8sv1.PodRunning {
					return fmt.Errorf("Waiting on rook-tools-pod with phase %s to be %s", pods.Items[0].Status.Phase, k8sv1.PodRunning)
				}
				// pod is online and running
				return nil
			}, 200*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

			By("Setting enableCephTools=false")
			patchOcsInit(disableToolsPatch)

			By("Ensuring tools are removed")
			Eventually(func() error {
				pods, err := k8sClient.CoreV1().Pods(deploymanager.InstallNamespace).List(metav1.ListOptions{LabelSelector: "app=rook-ceph-tools"})
				if err != nil {
					return err
				}

				if len(pods.Items) != 0 {
					return fmt.Errorf("waiting for rook-tools-pod to be deleted")
				}

				// pod is removed
				return nil
			}, 200*time.Second, 1*time.Second).ShouldNot(HaveOccurred())

		})
	})
})
