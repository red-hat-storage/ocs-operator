package functests

import (
	"github.com/onsi/gomega"

	"encoding/json"
	"fmt"
	"time"

	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"

	ocsv1 "github.com/openshift/ocs-operator/pkg/apis/ocs/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/strategicpatch"
)

func testStorageCluster() *ocsv1.StorageCluster {
	monQuantity, err := resource.ParseQuantity("10Gi")
	gomega.Expect(err).To(gomega.BeNil())
	dataQuantity, err := resource.ParseQuantity("50Gi")
	gomega.Expect(err).To(gomega.BeNil())
	storageClassName := "gp2"
	blockVolumeMode := k8sv1.PersistentVolumeBlock
	storageCluster := &ocsv1.StorageCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name:      TestStorageCluster,
			Namespace: "openshift-storage",
		},
		Spec: ocsv1.StorageClusterSpec{
			ManageNodes: false,
			MonPVCTemplate: &k8sv1.PersistentVolumeClaim{
				Spec: k8sv1.PersistentVolumeClaimSpec{
					StorageClassName: &storageClassName,
					AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},

					Resources: k8sv1.ResourceRequirements{
						Requests: k8sv1.ResourceList{
							"storage": monQuantity,
						},
					},
				},
			},
			StorageDeviceSets: []ocsv1.StorageDeviceSet{
				{
					Name:     "example-deviceset",
					Count:    MinOSDsCount,
					Portable: true,
					DataPVCTemplate: k8sv1.PersistentVolumeClaim{
						Spec: k8sv1.PersistentVolumeClaimSpec{
							StorageClassName: &storageClassName,
							AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},
							VolumeMode:       &blockVolumeMode,

							Resources: k8sv1.ResourceRequirements{
								Requests: k8sv1.ResourceList{
									"storage": dataQuantity,
								},
							},
						},
					},
				},
			},
		},
	}
	storageCluster.SetGroupVersionKind(schema.GroupVersionKind{Group: ocsv1.SchemeGroupVersion.Group, Kind: "StorageCluster", Version: ocsv1.SchemeGroupVersion.Version})

	return storageCluster
}

// getStorageCluster retrieves the test suite storage cluster
func (t *TestClient) getStorageCluster() (*ocsv1.StorageCluster, error) {
	sc := &ocsv1.StorageCluster{}
	err := t.restClient.Get().
		Resource("storageclusters").
		Namespace(InstallNamespace).
		Name(TestStorageCluster).
		VersionedParams(&metav1.GetOptions{}, t.parameterCodec).
		Do().
		Into(sc)

	if err != nil {
		return nil, err
	}

	return sc, nil
}

// createStorageCluster is used to install the test suite storage cluster
func (t *TestClient) createStorageCluster() (*ocsv1.StorageCluster, error) {
	newSc := &ocsv1.StorageCluster{}

	sc := testStorageCluster()
	err := t.restClient.Post().
		Resource("storageclusters").
		Namespace(InstallNamespace).
		Name(sc.Name).
		Body(sc).
		Do().
		Into(newSc)

	if err != nil {
		return nil, err
	}

	return newSc, nil
}

func (t *TestClient) waitOnStorageCluster() {
	// wait for storage cluster to come online
	gomega.Eventually(func() error {
		sc, err := t.getStorageCluster()
		if err != nil {
			return err
		}

		_true := true
		_false := false
		var available *bool
		var upgradeable *bool
		var progressing *bool
		var degraded *bool

		for _, condition := range sc.Status.Conditions {
			switch condition.Type {
			case conditionsv1.ConditionAvailable:
				available = &_false
				if condition.Status == k8sv1.ConditionTrue {
					available = &_true
				}
			case conditionsv1.ConditionProgressing:
				progressing = &_false
				if condition.Status == k8sv1.ConditionTrue {
					progressing = &_true
				}
			case conditionsv1.ConditionDegraded:
				degraded = &_false
				if condition.Status == k8sv1.ConditionTrue {
					degraded = &_true
				}
			case conditionsv1.ConditionUpgradeable:
				upgradeable = &_false
				if condition.Status == k8sv1.ConditionTrue {
					upgradeable = &_true
				}
			}
		}

		// we have to wait for all of these conditions to exist as well as be set
		if available == nil {
			return fmt.Errorf("Waiting on 'available' condition to be set")
		} else if upgradeable == nil {
			return fmt.Errorf("Waiting on 'upgradeable' condition to be set")
		} else if progressing == nil {
			return fmt.Errorf("Waiting on 'progressing' condition to be set")
		} else if degraded == nil {
			return fmt.Errorf("Waiting on 'degraded' condition to be set")
		}

		if !*available || !*upgradeable || *degraded || *progressing {
			return fmt.Errorf("waiting on storage cluster to come online. available: %t, upgradeable: %t, progressing: %t, degraded: %t",
				*available,
				*upgradeable,
				*progressing,
				*degraded)
		}

		// We expect at least 3 osd deployments to be online and available
		deployments, err := t.k8sClient.AppsV1().Deployments(InstallNamespace).List(metav1.ListOptions{LabelSelector: "app=rook-ceph-osd"})
		if err != nil {
			return err
		}
		osdsOnline := 0
		for _, deployment := range deployments.Items {
			expectedReplicas := int32(1)
			if deployment.Spec.Replicas != nil {
				expectedReplicas = *deployment.Spec.Replicas
			}

			if expectedReplicas == deployment.Status.ReadyReplicas {
				osdsOnline++
			}
		}

		if osdsOnline < MinOSDsCount {
			return fmt.Errorf("%d/%d expected OSDs are online", osdsOnline, MinOSDsCount)
		}

		return nil

	}, 1200*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
	// NOTE the long timeout above. It can take quite a bit of time for this
	// storage cluster to fully initialize
}

func (t *TestClient) labelWorkerNodes() {
	nodes, err := t.k8sClient.CoreV1().Nodes().List(metav1.ListOptions{LabelSelector: "node-role.kubernetes.io/worker"})
	gomega.Expect(err).ToNot(gomega.HaveOccurred())

	labeledCount := MinOSDsCount

	for _, node := range nodes.Items {
		old, err := json.Marshal(node)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		new := node.DeepCopy()
		new.Labels["cluster.ocs.openshift.io/openshift-storage"] = ""

		newJSON, err := json.Marshal(new)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		patch, err := strategicpatch.CreateTwoWayMergePatch(old, newJSON, node)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		_, err = t.k8sClient.CoreV1().Nodes().Patch(node.Name, types.StrategicMergePatchType, patch)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())

		labeledCount--
		if labeledCount == 0 {
			break
		}
	}

	gomega.Expect(labeledCount).To(gomega.Equal(0))
}

func (t *TestClient) startStorageCluster() {
	// Ensure storage cluster is created
	_, err := t.createStorageCluster()
	if err != nil && !errors.IsAlreadyExists(err) {
		gomega.Expect(err).To(gomega.BeNil())
	}
}

func (t *TestClient) createNamespaces() {
	// Create a Test Namespaces
	for _, namespace := range namespaces {
		ns := &k8sv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
			},
		}
		_, err := t.k8sClient.CoreV1().Namespaces().Create(ns)
		if err != nil && !errors.IsAlreadyExists(err) {
			gomega.Expect(err).To(gomega.BeNil())
		}
	}
}

// BeforeTestSuiteSetup is the function called to initialize the test environment
func BeforeTestSuiteSetup() {
	t := NewTestClient()

	// create the namespaces we'll work with
	t.createNamespaces()

	// label the worker nodes with the storage label
	t.labelWorkerNodes()

	// start the storage cluster
	t.startStorageCluster()

	// Ensure storage cluster is online before starting tests
	t.waitOnStorageCluster()
}

// AfterTestSuiteCleanup is the function called to tear down the test environment
func AfterTestSuiteCleanup() {
	t := NewTestClient()

	// delete the namespace we used to test PVCs and workloads.
	err := t.k8sClient.CoreV1().Namespaces().Delete(TestNamespace, &metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		gomega.Expect(err).To(gomega.BeNil())
	}

	// Wait for namespace to terminate
	gomega.Eventually(func() error {
		_, err = t.k8sClient.CoreV1().Namespaces().Get(TestNamespace, metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			return fmt.Errorf("Still waiting on namespace %s to terminate after suite", TestNamespace)
		}
		return nil
	}, 200*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())

	// TODO uninstall storage cluster.
	// Right now uninstall doesn't work. Once uninstall functions
	// properly, we'll want to uninstall the storage cluster after
	// the testsuite completes
}
