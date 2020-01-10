/*
informer is based on the shared informers package to monitor the deployments in
the openshift-storage namespace and filter the events down to monitoring the
status of the ocs-operator deployment. Can be used in conjunction with the
`killpod` executable to monitor the status of the ocs-operator deployment.

Run it with `go run` alongside negativetest.
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	appsv1 "k8s.io/api/apps/v1"
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
		UpdateFunc: func(oldObj, newObj interface{}) {
			oldD := oldObj.(*appsv1.Deployment)
			newD := newObj.(*appsv1.Deployment)

			if oldD.Name != ocsOperatorName {
				return
			} else if newD.Name != ocsOperatorName {
				return
			}
			fmt.Printf("Updated deployment %s\n", oldD.Name)
			for i, c := range newD.Status.Conditions {
				fmt.Printf("\tCondition %d: %s\n", i, c.String())
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
