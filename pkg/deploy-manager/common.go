package deploymanager

import (
	"fmt"
	"time"

	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	types "k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
)

// CreateNamespace creates a namespace in the cluster, ignoring if it already exists
func (t *DeployManager) CreateNamespace(namespace string) error {
	label := make(map[string]string)
	// Label required for monitoring this namespace
	label["openshift.io/cluster-monitoring"] = "true"
	ns := &k8sv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name:   namespace,
			Labels: label,
		},
	}
	_, err := t.k8sClient.CoreV1().Namespaces().Create(ns)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// DeleteStorageClusterAndWait deletes a storageClusterCR and waits on it to terminate
func (t *DeployManager) DeleteStorageClusterAndWait(namespace string) error {
	err := t.deleteStorageCluster()
	if err != nil && !errors.IsNotFound(err) {
		return err
	}
	cephClusters, err := t.rookClient.CephV1().CephClusters(namespace).List(metav1.ListOptions{})
	if err != nil {
		return err
	}
	for _, cephCluster := range cephClusters.Items {
		_, err = t.rookClient.CephV1().CephClusters(namespace).Patch(cephCluster.GetName(), types.JSONPatchType, []byte(finalizerRemovalPatch))
		if err != nil {
			return err
		}
	}

	timeout := 600 * time.Second
	interval := 10 * time.Second

	// Wait for storagecluster and cephCluster to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		cephClusters, err := t.rookClient.CephV1().CephClusters(namespace).List(metav1.ListOptions{})
		if err != nil {
			return false, err
		}
		if len(cephClusters.Items) != 0 {
			return false, nil
		}
		_, err = t.getStorageCluster()
		if !errors.IsNotFound(err) {
			return false, nil
		}
		return true, nil
	})

	return err
}

// DeleteNamespaceAndWait deletes a namespace and waits on it to terminate
func (t *DeployManager) DeleteNamespaceAndWait(namespace string) error {
	err := t.DeleteStorageClusterAndWait(namespace)
	if err != nil {
		return err
	}
	err = t.k8sClient.CoreV1().Namespaces().Delete(namespace, &metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	timeout := 600 * time.Second
	interval := 10 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		_, err = t.k8sClient.CoreV1().Namespaces().Get(namespace, metav1.GetOptions{})
		if !errors.IsNotFound(err) {
			return false, nil
		}
		return true, nil
	})

	return err
}

// GetDeploymentImage returns the deployment image name for the deployment
func (t *DeployManager) GetDeploymentImage(name string) (string, error) {
	deployment, err := t.k8sClient.AppsV1().Deployments(InstallNamespace).Get(name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return deployment.Spec.Template.Spec.Containers[0].Image, nil
}

// WaitForPVCBound waits for a pvc with a given name and namespace to reach BOUND phase
func (t *DeployManager) WaitForPVCBound(pvc *k8sv1.PersistentVolumeClaim, namespace string) error {
	pvc, err := t.k8sClient.CoreV1().PersistentVolumeClaims(namespace).Create(pvc)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	lastReason := ""
	timeout := 100 * time.Second
	interval := 1 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		pvc, err := t.k8sClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(pvc.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if pvc.Status.Phase != k8sv1.ClaimBound {
			lastReason = fmt.Sprintf("waiting on pvc %s/%s to reach bound state, currently %s", pvc.Namespace, pvc.Name, pvc.Status.Phase)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}

// WaitForJobSucceeded waits for a Job with a given name and namespace to succeed until 200 seconds
func (t *DeployManager) WaitForJobSucceeded(job *batchv1.Job, namespace string) error {
	job, err := t.k8sClient.BatchV1().Jobs(namespace).Create(job)
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	lastReason := ""
	timeout := 200 * time.Second
	interval := 1 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		job, err := t.k8sClient.BatchV1().Jobs(job.Namespace).Get(job.Name, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			lastReason = fmt.Sprintf("error talking to k8s apiserver: %v", err)
			return false, nil
		}

		if job.Status.Active != 0 {
			lastReason = fmt.Sprintf("waiting on job %s/%s to succeed, currently Active", job.Namespace, job.Name)
			return false, nil
		}

		if job.Status.Failed != 0 {
			lastReason = fmt.Sprintf("waiting on job %s/%s to succeed, currently Failed", job.Namespace, job.Name)
			return false, nil
		}

		if job.Status.Succeeded == 0 {
			lastReason = fmt.Sprintf("waiting on job %s/%s to succeed", job.Namespace, job.Name)
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}
