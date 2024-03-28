package functests

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"

	k8sbatchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

//nolint:errcheck
func debug(msg string, args ...interface{}) {
	ginkgo.GinkgoWriter.Write([]byte(fmt.Sprintf(msg, args...)))
}

// RunMustGather runs the OCS must-gather container image
func RunMustGather() error {
	cmd := exec.Command("/bin/bash", "hack/dump-debug-info.sh")
	cmd.Env = os.Environ()
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("error dumping debug info: %v", string(output))
	}

	return nil
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

			Resources: k8sv1.VolumeResourceRequirements{
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
						{
							Name:  randomName,
							Image: "busybox",
							VolumeMounts: []k8sv1.VolumeMount{
								{
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
						{
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
