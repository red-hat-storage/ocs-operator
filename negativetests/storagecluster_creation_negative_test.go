package negativetests_test

import (
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/util"
	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
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

	const (
		ocsOperatorNamespace = deploymanager.InstallNamespace
		ocsOperatorName      = "ocs-operator"
		ocsOperatorResource  = "storageclusters"
		ocsPodLabelSelector  = "name=ocs-operator"
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
			Resource(ocsOperatorResource).
			Namespace(ocsOperatorNamespace).
			Name(deploymanager.DefaultStorageClusterName).
			VersionedParams(&metav1.GetOptions{}, parameterCodec).
			Do().
			Error()

		Expect(err).To(BeNil())
	})

	It("Interrupts and Verifies StorageCluster Creation", func() {
		ocsClient = d.GetOcsClient()
		parameterCodec = d.GetParameterCodec()

		// This should eventually be able to use ocsClient. For now, I don't know
		// how to use custom resource clients.
		k8sClient := d.GetK8sClient()

		By("Creating StorageCluster")
		err := d.StartDefaultStorageCluster()
		Expect(err).To(BeNil())

		By("Waiting for the ocs-operator deployment to be available")
		// We first fetch the deployment object. It may not exist in the
		// beginning.
		Eventually(func() (bool, error) {
			deployment, dErr := k8sClient.AppsV1().Deployments(ocsOperatorNamespace).Get(ocsOperatorName, metav1.GetOptions{})
			if dErr != nil {
				fmt.Fprintln(GinkgoWriter, dErr.Error())
				return false, dErr
			}
			// deployment object exists.
			fmt.Fprintf(GinkgoWriter, "ocs-operator deployment's name is: %s\n", deployment.Name)

			// Now we check if the deployment is in the "Available" condition
			for _, c := range deployment.Status.Conditions {
				if c.Type == appsv1.DeploymentAvailable {
					fmt.Fprintf(GinkgoWriter, "DeploymentAvailable condition: %s.\n", c.String())
					if c.Status == corev1.ConditionTrue {
						return true, nil
					}
				}
			}
			return false, nil
		}, 10*time.Minute, 10*time.Second).Should(BeTrue())

		By("Fetching the ocs-operator pod")
		podList, pErr := k8sClient.CoreV1().Pods(ocsOperatorNamespace).List(metav1.ListOptions{LabelSelector: ocsPodLabelSelector})
		Expect(pErr).To(BeNil())
		Expect(len(podList.Items)).ToNot(BeZero())

		By("Deleting the fetched pods")
		for _, p := range podList.Items {
			var (
				n           = p.Name
				gracePeriod = int64(0)
			)
			fmt.Fprintf(GinkgoWriter, "Killing pod %s immediately.\n", n)
			deleteErr := k8sClient.CoreV1().Pods(ocsOperatorNamespace).Delete(n, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
			Expect(deleteErr).To(BeNil())
			fmt.Fprintf(GinkgoWriter, "Verifying that the pod %s was killed.\n", n)
			_, gErr := k8sClient.CoreV1().Pods(ocsOperatorNamespace).Get(n, metav1.GetOptions{})
			Expect(apierrors.IsNotFound(gErr)).To(BeTrue())
		}

		By("Verifying StorageCluster is PhaseReady")
		sc := &ocsv1.StorageCluster{}
		Eventually(func() error {
			err := ocsClient.Get().
				Resource(ocsOperatorResource).
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
