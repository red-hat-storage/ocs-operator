package main

import (
	"context"
	"os"
	"time"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v1"
	"github.com/red-hat-storage/ocs-operator/controllers/storagecluster"
	providerclient "github.com/red-hat-storage/ocs-operator/services/provider/client"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func main() {
	scheme := runtime.NewScheme()
	if err := ocsv1.AddToScheme(scheme); err != nil {
		klog.Exitf("Failed to add ocsv1 to scheme: %v", err)
	}

	config, err := config.GetConfig()
	if err != nil {
		klog.Exitf("Failed to get config: %v", err)
	}
	cl, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		klog.Exitf("Failed to create controller-runtime client: %v", err)
	}

	ctx := context.Background()

	namespace, isSet := os.LookupEnv(storagecluster.NamespaceEnvVar)
	if !isSet {
		klog.Exitf("%s env var not set", storagecluster.NamespaceEnvVar)
	}

	storageClusterList := &ocsv1.StorageClusterList{}
	if err := cl.List(ctx, storageClusterList, client.InNamespace(namespace)); err != nil {
		klog.Exitf("Failed to list storageClusters: %v", err)
	}

	switch l := len(storageClusterList.Items); {
	case l == 0:
		klog.Exitf("No StorageCluster found")
	case l > 1:
		klog.Exitf("Multiple StorageCluster found")
	}

	storageCluster := &storageClusterList.Items[0]

	providerClient, err := providerclient.NewProviderClient(
		ctx,
		storageCluster.Spec.ExternalStorage.StorageProviderEndpoint,
		10*time.Second,
	)
	if err != nil {
		klog.Exitf("Failed to create grpc client: %v", err)
	}
	defer providerClient.Close()

	if _, err = providerClient.ReportStatus(ctx, storageCluster.Status.ExternalStorage.ConsumerID); err != nil {
		klog.Exitf("Failed to update lastHeartbeat of storageConsumer %v: %v", storageCluster.Status.ExternalStorage.ConsumerID, err)
	}

}
