/*
informer is based on the shared informers package to monitor the deployments in
the openshift-storage namespace and filter the events down to monitoring the
status of the ocs-operator deployment. Can be used in conjunction with the
`killpod` executable to monitor the status of the ocs-operator deployment.

Run it with `go run` alongside negativetest.
*/
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	ocsOperatorNamespace = "openshift-storage"
	ocsOperatorName      = "ocs-operator"
	ocsOperatorResource  = "deployments"
	ocsPodLabelSelector  = "name=ocs-operator"
)

func main() {
	var kubeconfig *string
	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()

	// use the current context in kubeconfig
	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	d, err := clientset.AppsV1().Deployments(ocsOperatorNamespace).Get(ocsOperatorName, metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("ocs-operator deployment's name is: %s\n", d.Name)

	factory := informers.NewSharedInformerFactory(clientset, 0)
	informer := factory.Apps().V1().Deployments().Informer()
	stopper := make(chan struct{})
	defer close(stopper)
	informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		/*
			AddFunc: func(obj interface{}) {
				d := obj.(*appsv1.Deployment)
				if d.Name != ocsOperatorName {
					return
				}
				fmt.Printf("Added deployment %s\n", d.Name)
				return
			},
			DeleteFunc: func(obj interface{}) {
				d := obj.(*appsv1.Deployment)
				if d.Name != ocsOperatorName {
					return
				}
				fmt.Printf("Deleted deployment %s\n", d.Name)
				return
			},
		*/
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldD := oldObj.(*appsv1.Deployment)
			newD := newObj.(*appsv1.Deployment)

			if oldD.Name != ocsOperatorName {
				return
			} else if newD.Name != ocsOperatorName {
				return
			}
			fmt.Printf("Updated deployment %s\n", oldD.Name)
			for _, c := range newD.Status.Conditions {
				if c.Type == appsv1.DeploymentAvailable {
					fmt.Printf("\tCondition: %s\n", c.String())
					if c.Status == corev1.ConditionTrue {
						fmt.Println("Invoking killpod()")
						killerr := killpod(clientset)
						if killerr != nil {
							fmt.Println(err.Error())
						}
					}
				}
			}
		},
	})
	informer.Run(stopper)
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func killpod(clientset *kubernetes.Clientset) error {
	pod, err := clientset.CoreV1().Pods(ocsOperatorNamespace).List(metav1.ListOptions{LabelSelector: ocsPodLabelSelector})
	if err != nil {
		return err
	}

	if len(pod.Items) < 1 {
		strNoRunningPods := "No running pods found."
		fmt.Println(strNoRunningPods)
		return errors.New(strNoRunningPods)
	}

	for _, p := range pod.Items {
		var (
			n           = p.Name
			gracePeriod = int64(0)
		)
		fmt.Printf("Killing pod %s now!\n", n)

		e := clientset.CoreV1().Pods(ocsOperatorNamespace).Delete(n, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
		if e != nil {
			return e
		}
		fmt.Printf("Pod %s killed. Verifying.\n", n)

		_, err := clientset.CoreV1().Pods(ocsOperatorNamespace).Get(n, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			fmt.Printf("Pod %s not found. Successfully killed, then.\n", n)
		} else if statusError, isStatus := err.(*apierrors.StatusError); isStatus {
			fmt.Printf("Error getting pod %s: %v\n", n, statusError.ErrStatus.Message)
		} else if err != nil {
			return err
		} else {
			strPodFound := fmt.Sprintf("Found pod %s, this shouldn't be!\n", n)
			errors.New(strPodFound)
		}
	}

	return nil
}
