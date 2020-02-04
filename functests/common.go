package functests

import (
	"github.com/onsi/gomega"

	"fmt"
	"time"

	k8sbatchv1 "k8s.io/api/batch/v1"
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

// WaitForJobSucceeded waits for a Job with a given name and namespace to succeed until 200 seconds
func WaitForJobSucceeded(k8sClient *kubernetes.Clientset, jobName string, jobNamespace string) {
	gomega.Eventually(func() error {
		job, err := k8sClient.BatchV1().Jobs(jobNamespace).Get(jobName, metav1.GetOptions{})
		if err != nil {
			return err
		}

		if job.Status.Succeeded > 0 {
			return nil
		}
		return fmt.Errorf("Waiting on job %s/%s to succeed when it is currently %d", jobName, jobNamespace, job.Status.Succeeded)
	},
		200*time.Second, 1*time.Second).Should(gomega.Succeed())
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

// GetDataValidatorJob returns the spec of a job
func GetDataValidatorJob(pvc string) *k8sbatchv1.Job {
	randomName := "test-job-" + rand.String(12)
	job := &k8sbatchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name: randomName,
		},
		Spec: k8sbatchv1.JobSpec{
			Template: k8sv1.PodTemplateSpec{
				Spec: k8sv1.PodSpec{
					RestartPolicy: k8sv1.RestartPolicyNever,
					Containers: []k8sv1.Container{
						k8sv1.Container{
							Name:  randomName,
							Image: "busybox",
							VolumeMounts: []k8sv1.VolumeMount{
								k8sv1.VolumeMount{
									MountPath: "/data",
									Name:      "volume-to-debug",
								},
							},
							Command: []string{"/bin/sh", "-c"},
							Args: []string{
								"dd if=/dev/zero of=/tmp/random.img bs=512 count=1",       //This command creates new file named random.img
								"md5VAR1=$(md5sum /tmp/random.img | awk '{ print $1 }')",  //calculates md5sum of random.img and stores it in a variable
								"cp /tmp/random.img /data/random.img",                     //copies random.img file to pvc's mountpoint
								"md5VAR2=$(md5sum /data/random.img | awk '{ print $1 }')", //calculates md5sum of file random.img
								"if [[ \"$md5VAR1\" != \"$md5VAR2\" ]];then exit 1; fi",   //compares the md5sum of random.img file with previous one
							},
						},
					},
					Volumes: []k8sv1.Volume{
						k8sv1.Volume{
							Name: "volume-to-debug",
							VolumeSource: k8sv1.VolumeSource{
								PersistentVolumeClaim: &k8sv1.PersistentVolumeClaimVolumeSource{
									ClaimName: pvc,
								},
							},
						},
					},
				},
			},
		},
	}
	return job
}
