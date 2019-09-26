package functests

import (
	"github.com/onsi/gomega"

	"fmt"
	"time"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
	"k8s.io/client-go/kubernetes"
)

// WaitForPVCBound waits for a pvc with a given name and namespace to reach BOUND phase
func WaitForPVCBound(k8sClient *kubernetes.Clientset, pvcName string, pvcNamespace string) {
	gomega.Eventually(func() error {
		pvc, err := k8sClient.CoreV1().PersistentVolumeClaims(pvcNamespace).Get(pvcName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if pvc.Status.Phase == k8sv1.ClaimBound {
			return nil
		}
		return fmt.Errorf("Waiting on pvc %s/%s to reach bound state when it is currently %s", pvcNamespace, pvcName, pvc.Status.Phase)
	}, 200*time.Second, 1*time.Second).ShouldNot(gomega.HaveOccurred())
}

// GetRandomPVC returns a pvc with a randomized name
func GetRandomPVC(storageClass string, quantity string) *k8sv1.PersistentVolumeClaim {
	storageQuantity, err := resource.ParseQuantity(quantity)
	gomega.Expect(err).To(gomega.BeNil())

	randomName := "test-pvc-" + rand.String(12)

	pvc := &k8sv1.PersistentVolumeClaim{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "PersistentVolumeClaim",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: randomName,
		},
		Spec: k8sv1.PersistentVolumeClaimSpec{
			StorageClassName: &storageClass,
			AccessModes:      []k8sv1.PersistentVolumeAccessMode{k8sv1.ReadWriteOnce},

			Resources: k8sv1.ResourceRequirements{
				Requests: k8sv1.ResourceList{
					"storage": storageQuantity,
				},
			},
		},
	}

	return pvc
}
