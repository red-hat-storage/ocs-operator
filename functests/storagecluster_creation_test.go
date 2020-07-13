package functests_test

import (
	"bytes"
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	"github.com/openshift/ocs-operator/pkg/controller/util"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	deploymanager "github.com/openshift/ocs-operator/pkg/deploy-manager"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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

var _ = Describe("Test PDB Alert Inhibition", func() {
	var client client.Client
	var storageCluster1 *ocsv1.StorageCluster
	var storageCluster2 *ocsv1.StorageCluster
	alertmanagerConfigSecret := &corev1.Secret{}
	alertmanagerConfigSecretName := "alertmanager-main"
	alertmanagerConfigSecretNamespace := "openshift-monitoring"
	// DO NOT EDIT THE FORMATTING OF THIS STRING
	// Whitespaces are important as it is directly converted into YAML
	inhibitRules := `
	- "equal":
	  - "namespace"
	  - "alertname"
	  "source_match_re":
	   "poddisruptionbudget" : "^rook-ceph-osd-[0-9]+$"
	  "target_match_re":
	    "poddisruptionbudget" : "^rook-ceph-osd-[0-9]+$"`

	BeforeEach(func() {
		RegisterFailHandler(Fail)

		deployManager, err := deploymanager.NewDeployManager()
		Expect(err).To(BeNil(), "Error: %+v", err)
		defaultStorageCluster, err := deploymanager.DefaultStorageCluster()
		Expect(err).To(BeNil(), "Error: %+v", err)

		storageCluster1 = defaultStorageCluster.DeepCopy()
		storageCluster1.Name = "ocs-storagecluster-1"
		storageCluster2 = defaultStorageCluster.DeepCopy()
		storageCluster2.Name = "ocs-storagecluster-2"

		client = deployManager.GetCrClient()
	})

	Context("after StorageCluster creation", func() {
		It("verify PDB Alert Inhibition is added", func() {
			By("creating first StorageCluster & looking at Alertmanager Config", func() {
				err := client.Create(context.TODO(), storageCluster1)
				Expect(err).To(BeNil(), "Error: %+v", err)
				err = client.Get(context.TODO(), types.NamespacedName{Name: alertmanagerConfigSecretName, Namespace: alertmanagerConfigSecretNamespace}, alertmanagerConfigSecret)
				Expect(err).To(BeNil(), "Error: %+v", err)
				Expect(bytes.Contains(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules))).To(BeTrue(), "Alertmanager config doesn't contain PDB alert inhibition rule. Found: \n%s", string(alertmanagerConfigSecret.Data["alertmanager.yaml"]))
				length := len(bytes.SplitAfter(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules)))
				Expect(length).To(Equal(2), "Alertmanager config contains multiple PDB alert inhibition rules. Expected exactly 1.")
			})
		})

		It("verify PDB Alert Inhibition is not changed", func() {
			By("creating second StorageCluster & looking at Alertmanager Config", func() {
				err := client.Create(context.TODO(), storageCluster2)
				Expect(err).To(BeNil(), "Error: %+v", err)
				err = client.Get(context.TODO(), types.NamespacedName{Name: alertmanagerConfigSecretName, Namespace: alertmanagerConfigSecretNamespace}, alertmanagerConfigSecret)
				Expect(err).To(BeNil(), "Error: %+v", err)
				Expect(bytes.Contains(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules))).To(BeTrue(), "Alertmanager config doesn't contain PDB alert inhibition rule. Found: \n%s", string(alertmanagerConfigSecret.Data["alertmanager.yaml"]))
				length := len(bytes.SplitAfter(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules)))
				Expect(length).To(Equal(2), "Alertmanager config contains multiple PDB alert inhibition rules. Expected exactly 1.")
			})
		})
	})

	Context("after StorageCluster deletion", func() {
		It("verify PDB Alert Inhibition is not changed", func() {
			By("deleting first StorageCluster & looking at Alertmanager Config", func() {
				err := client.Delete(context.TODO(), storageCluster1)
				Expect(err).To(BeNil(), "Error: %+v", err)
				err = client.Get(context.TODO(), types.NamespacedName{Name: alertmanagerConfigSecretName, Namespace: alertmanagerConfigSecretNamespace}, alertmanagerConfigSecret)
				Expect(err).To(BeNil(), "Error: %+v", err)
				Expect(bytes.Contains(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules))).To(BeTrue(), "Alertmanager config doesn't contain PDB alert inhibition rule. Found: \n%s", string(alertmanagerConfigSecret.Data["alertmanager.yaml"]))
				length := len(bytes.SplitAfter(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules)))
				Expect(length).To(Equal(2), "Alertmanager config contains multiple PDB alert inhibition rules. Expected exactly 1.")
			})
		})

		It("verify PDB Alert Inhibition is removed", func() {
			By("deleting second StorageCluster & looking at Alertmanager Config", func() {
				err := client.Delete(context.TODO(), storageCluster2)
				Expect(err).To(BeNil(), "Error: %+v", err)
				err = client.Get(context.TODO(), types.NamespacedName{Name: alertmanagerConfigSecretName, Namespace: alertmanagerConfigSecretNamespace}, alertmanagerConfigSecret)
				Expect(err).To(BeNil(), "Error: %+v", err)
				Expect(bytes.Contains(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules))).To(BeFalse(), "Alertmanager config still contains PDB alert inhibition rule. Found: \n%s", string(alertmanagerConfigSecret.Data["alertmanager.yaml"]))
				length := len(bytes.SplitAfter(alertmanagerConfigSecret.Data["alertmanager.yaml"], []byte(inhibitRules)))
				Expect(length).To(Equal(1), "Alertmanager config contains PDB alert inhibition rule. Expected none.")
			})
		})
	})
})
