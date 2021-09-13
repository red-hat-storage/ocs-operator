package deploymanager

import (
	"context"
	"fmt"
	"strings"
	"time"

	nbv1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	rookcephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"

	batchv1 "k8s.io/api/batch/v1"
	k8sv1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	utilwait "k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
	_, err := t.k8sClient.CoreV1().Namespaces().Create(context.TODO(), ns, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

// RemoveAllFinalizers removes all finalizers from every object in the target namespace
func (t *DeployManager) RemoveAllFinalizers(namespace string) error {
	finalPatch := client.RawPatch(types.JSONPatchType, []byte(finalizerRemovalPatch))
	gvs := []schema.GroupVersion{
		rookcephv1.SchemeGroupVersion,
		ocsv1.GroupVersion,
		nbv1.SchemeGroupVersion,
	}

	for _, gv := range gvs {
		kinds := scheme.Scheme.KnownTypes(gv)
		for kind := range kinds {
			// For some reason, meta Kinds get added to each project's scheme. Skip
			// those and Lists Kinds.
			if strings.Contains(kind, "Option") || strings.Contains(kind, "Event") || strings.Contains(kind, "List") {
				continue
			}
			objs := &unstructured.UnstructuredList{}
			objs.SetGroupVersionKind(gv.WithKind(kind))
			err := t.GetCrClient().List(context.TODO(), objs, &client.ListOptions{Namespace: namespace})
			if err != nil {
				return err
			}
			for _, obj := range objs.Items {
				finalizers := obj.GetFinalizers()
				if len(finalizers) > 0 {
					_ = t.GetCrClient().Patch(context.TODO(), &obj, finalPatch)
				}
			}
		}
	}

	return nil
}

// DeleteStorageClusterAndWait deletes a storageClusterCR and waits on it to terminate
func (t *DeployManager) DeleteStorageClusterAndWait(namespace string) error {
	err := t.deleteStorageCluster()
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	lastReason := ""
	timeout := 600 * time.Second
	interval := 10 * time.Second

	// Wait for storagecluster and cephCluster to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		cephClusters, err := t.rookClient.CephV1().CephClusters(namespace).List(context.TODO(), metav1.ListOptions{})
		if err != nil {
			lastReason = fmt.Sprintf("Error talking to k8s apiserver: %v", err)
			return false, err
		}
		if len(cephClusters.Items) != 0 {
			lastReason = "Waiting on CephClusters to be deleted"
			return false, nil
		}
		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	err = t.RemoveAllFinalizers(namespace)
	if err != nil {
		return err
	}

	err = t.GetCrClient().DeleteAllOf(context.TODO(), &k8sv1.PersistentVolumeClaim{}, client.InNamespace(InstallNamespace))
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	return nil
}

// DeleteNamespaceAndWait deletes a namespace and waits on it to terminate
func (t *DeployManager) DeleteNamespaceAndWait(namespace string) error {
	err := t.k8sClient.CoreV1().Namespaces().Delete(context.TODO(), namespace, metav1.DeleteOptions{})
	if err != nil && !errors.IsNotFound(err) {
		return err
	}

	lastReason := ""
	timeout := 600 * time.Second
	interval := 10 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		_, err = t.k8sClient.CoreV1().Namespaces().Get(context.TODO(), namespace, metav1.GetOptions{})
		if err != nil && !errors.IsNotFound(err) {
			lastReason = fmt.Sprintf("Error talking to k8s apiserver: %v", err)
			return false, nil
		}
		if err == nil {
			lastReason = "Waiting on namespace to be deleted"
			return false, nil
		}

		return true, nil
	})

	if err != nil {
		return fmt.Errorf("%v: %s", err, lastReason)
	}

	return nil
}

// GetDeploymentImage returns the deployment image name for the deployment
func (t *DeployManager) GetDeploymentImage(name string) (string, error) {
	deployment, err := t.k8sClient.AppsV1().Deployments(InstallNamespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return "", err
	}
	return deployment.Spec.Template.Spec.Containers[0].Image, nil
}

// WaitForPVCBound waits for a pvc with a given name and namespace to reach BOUND phase
func (t *DeployManager) WaitForPVCBound(pvc *k8sv1.PersistentVolumeClaim, namespace string) error {
	pvc, err := t.k8sClient.CoreV1().PersistentVolumeClaims(namespace).Create(context.TODO(), pvc, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	lastReason := ""
	timeout := 100 * time.Second
	interval := 1 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		pvc, err := t.k8sClient.CoreV1().PersistentVolumeClaims(pvc.Namespace).Get(context.TODO(), pvc.Name, metav1.GetOptions{})
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
	job, err := t.k8sClient.BatchV1().Jobs(namespace).Create(context.TODO(), job, metav1.CreateOptions{})
	if err != nil && !errors.IsAlreadyExists(err) {
		return err
	}

	lastReason := ""
	timeout := 200 * time.Second
	interval := 1 * time.Second

	// Wait for namespace to terminate
	err = utilwait.PollImmediate(interval, timeout, func() (done bool, err error) {
		job, err := t.k8sClient.BatchV1().Jobs(job.Namespace).Get(context.TODO(), job.Name, metav1.GetOptions{})
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
