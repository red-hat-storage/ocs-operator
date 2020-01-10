/*
killpod is used to kill the ocs-operator pod if the ocs-operator deployment is
running. It errors out if the deployment is not found. For now, this can be
used to manually simulate the negative tests which deal with the ocs-operator
pod getting killed.

run with `go run`
*/
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

	pod, err := clientset.CoreV1().Pods("openshift-storage").List(metav1.ListOptions{LabelSelector: "name=ocs-operator"})
	if err != nil {
		panic(err.Error())
	}

	if len(pod.Items) < 1 {
		fmt.Println("No running pods founds.")
		os.Exit(1)
	}

	for _, p := range pod.Items {
		var (
			n           = p.Name
			gracePeriod = int64(0)
		)
		fmt.Printf("Killing pod %s now!\n", n)

		e := clientset.CoreV1().Pods("openshift-storage").Delete(n, &metav1.DeleteOptions{GracePeriodSeconds: &gracePeriod})
		if e != nil {
			panic(e.Error())
		}
		fmt.Printf("Pod %s killed. Verifying.\n", n)

		_, err := clientset.CoreV1().Pods("openshift-storage").Get(n, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			fmt.Printf("Pod %s not found. Successfully killed, then.\n", n)
		} else if statusError, isStatus := err.(*errors.StatusError); isStatus {
			fmt.Printf("Error getting pod %s: %v\n", n, statusError.ErrStatus.Message)
		} else if err != nil {
			panic(err.Error())
		} else {
			str := fmt.Sprintf("Found pod %s, this shouldn't be!\n", n)
			panic(str)
		}
	}
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}
