package tlsprofile

import (
	"context"
	"crypto/tls"

	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	ocstlsv1 "github.com/red-hat-storage/ocs-tls-profiles/api/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// GetConfig gets the centralized TLS profile configuration for metrics-exporter.
func GetConfig(ctx context.Context, restConfig *rest.Config, namespace string) (*tls.Config, error) {
	scheme := runtime.NewScheme()
	if err := ocstlsv1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	kubeClient, err := client.New(restConfig, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	return load(ctx, kubeClient, namespace)
}

func load(ctx context.Context, kubeClient client.Client, namespace string) (*tls.Config, error) {
	profile := &ocstlsv1.TLSProfile{}
	err := kubeClient.Get(ctx, types.NamespacedName{Name: defaults.TLSProfileName, Namespace: namespace}, profile)
	if err != nil && !apierrors.IsNotFound(err) {
		return nil, err
	}

	config, found := ocstlsv1.GetConfigForServer(profile, "ocs.openshift.io", "metrics-exporter")
	if !found {
		return &tls.Config{MinVersion: tls.VersionTLS12}, nil
	}
	return ocstlsv1.GetGoTLSConfig(config), nil
}
