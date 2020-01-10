/*
watcher uses the watch package to watch the ocs-operator deployment. This was
the initial code written to explore a way to monitor the ocs-operator
deployment status when killing the pod using killpod. This is redundent with
the informer implementation, but kept here as a reference for future use as
example implementation of the watcher package.

Run with `go run`.
*/

package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/davecgh/go-spew/spew"
	appsv1 "k8s.io/api/apps/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	//
	// Uncomment to load all auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth"
	//
	// Or uncomment to load specific auth plugins
	// _ "k8s.io/client-go/plugin/pkg/client/auth/azure"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/oidc"
	// _ "k8s.io/client-go/plugin/pkg/client/auth/openstack"
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

	//p, err := clientset.CoreV1().Pods("openshift-storage").List(metav1.ListOptions{LabelSelector: `name=ocs-operator`})
	//if err != nil {
	//	panic(err.Error())
	//}
	d, err := clientset.AppsV1().Deployments("openshift-storage").Get("ocs-operator", metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	fmt.Printf("ocs-operator deployment's name is: %s\n", d.Name)

	fieldSelector := "metadata.name=ocs-operator"
	listOpts := metav1.ListOptions{
		FieldSelector: fieldSelector,
	}

	dl, err := clientset.AppsV1().Deployments("openshift-storage").List(listOpts)
	if err != nil {
		panic(err.Error())
	}
	fmt.Printf("Number of ocs-operator deployments is: %d\n", len(dl.Items))

	spew.Dump(
		newDeploymentListWatchFromClient(clientset, listOpts),
	)

	watchlist := newDeploymentListWatchFromClient(clientset, listOpts)

	_, controller := cache.NewInformer(
		watchlist,
		&appsv1.Deployment{},
		0,
		cache.ResourceEventHandlerFuncs{
			AddFunc: func(obj interface{}) {
				fmt.Printf("Deployment added %s\n", obj.(*appsv1.Deployment).Name)
			},
			DeleteFunc: func(obj interface{}) {
				fmt.Printf("Deployment deleted: %s\n", obj.(*appsv1.Deployment).Name)
			},
			UpdateFunc: func(oldObj, newObj interface{}) {
				fmt.Printf("Deployment changed: %s\n", newObj.(*appsv1.Deployment).Name)
			},
		},
	)

	stop := make(chan struct{})
	defer close(stop)
	go controller.Run(stop)
	for {
		time.Sleep(time.Second)
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

func newDeploymentListWatchFromClient(c *kubernetes.Clientset, listOpts metav1.ListOptions) *cache.ListWatch {
	d := c.AppsV1().Deployments(ocsOperatorNamespace)
	listFunc := func(opts metav1.ListOptions) (runtime.Object, error) {
		l, e := d.List(opts)
		return l, e
	}
	watchFunc := func(opts metav1.ListOptions) (watch.Interface, error) {
		w, e := d.Watch(opts)
		return w, e
	}

	return &cache.ListWatch{ListFunc: listFunc, WatchFunc: watchFunc}
}
