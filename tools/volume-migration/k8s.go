package main

import (
	clientv1alpha1 "github.com/red-hat-storage/ocs-client-operator/api/v1alpha1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	ocsv1alpha1 "github.com/red-hat-storage/ocs-operator/api/v4/v1alpha1"
	rookclient "github.com/rook/rook/pkg/client/clientset/versioned"

	"k8s.io/apimachinery/pkg/runtime/schema"
	k8s "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

type Clientsets struct {
	kubeconfig *rest.Config

	Kube k8s.Interface
	Rook rookclient.Interface

	OcsV1          rest.Interface
	OcsV1alpha1    rest.Interface
	ClientV1alpha1 rest.Interface
}

func NewForConfigV1(c *rest.Config) (*rest.RESTClient, error) {
	config := *c
	config.ContentConfig.GroupVersion = &ocsv1.GroupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	v1Client, err := rest.UnversionedRESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return v1Client, nil
}

func NewForConfig(c *rest.Config, groupVersion schema.GroupVersion) (*rest.RESTClient, error) {
	config := *c
	config.ContentConfig.GroupVersion = &groupVersion
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	apiClient, err := rest.UnversionedRESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return apiClient, nil
}

func GetClientsets() *Clientsets {
	var err error

	clientsets := &Clientsets{}

	congfigOverride := &clientcmd.ConfigOverrides{}
	if KubeContext != "" {
		congfigOverride = &clientcmd.ConfigOverrides{CurrentContext: KubeContext}
	}

	clientsets.kubeconfig, err = rest.InClusterConfig()
	if err != nil {
		if err == rest.ErrNotInCluster {
			kubeconfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
				clientcmd.NewDefaultClientConfigLoadingRules(),
				congfigOverride,
			)

			clientsets.kubeconfig, err = kubeconfig.ClientConfig()
		}
		if err != nil {
			klog.Fatal(err)
		}
	}

	clientsets.Kube, err = k8s.NewForConfig(clientsets.kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}

	clientsets.Rook, err = rookclient.NewForConfig(clientsets.kubeconfig)
	if err != nil {
		klog.Fatal(err)
	}

	clientsets.OcsV1, err = NewForConfig(clientsets.kubeconfig, ocsv1.GroupVersion)
	if err != nil {
		klog.Fatal(err)
	}

	clientsets.OcsV1alpha1, err = NewForConfig(clientsets.kubeconfig, ocsv1alpha1.GroupVersion)
	if err != nil {
		klog.Fatal(err)
	}

	clientsets.ClientV1alpha1, err = NewForConfig(clientsets.kubeconfig, clientv1alpha1.GroupVersion)
	if err != nil {
		klog.Fatal(err)
	}

	return clientsets
}
