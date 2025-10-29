package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/bucket"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/expandstorage"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/featureflags"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/onboarding/peertokens"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/storages"

	noobaaapis "github.com/noobaa/noobaa-operator/v5/pkg/apis"
	nbv1alpha1 "github.com/noobaa/noobaa-operator/v5/pkg/apis/noobaa/v1alpha1"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	storagev1 "k8s.io/api/storage/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	ctrlcache "sigs.k8s.io/controller-runtime/pkg/cache"
	ctrlconfig "sigs.k8s.io/controller-runtime/pkg/client/config"
)

type serverConfig struct {
	listenPort           int
	tokenLifetimeInHours int
	tlsEnabled           bool
}

func loadAndValidateServerConfig() (*serverConfig, error) {
	var config serverConfig
	var err error

	config.tokenLifetimeInHours, err = util.ReadEnvVar("ONBOARDING_TOKEN_LIFETIME", 48, strconv.Atoi)
	if err != nil {
		return nil, err
	}

	config.listenPort, err = util.ReadEnvVar("UX_BACKEND_PORT", 8080, strconv.Atoi)
	if err != nil {
		return nil, err
	}

	config.tlsEnabled, err = util.ReadEnvVar("TLS_ENABLED", false, strconv.ParseBool)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func main() {
	klog.Info("Starting ux backend server")

	ctx := context.Background()

	config, err := loadAndValidateServerConfig()
	if err != nil {
		klog.Errorf("failed to load server config: %v", err)
		klog.Info("shutting down!")
		os.Exit(-1)
	}

	namespace := util.GetPodNamespace()

	scheme := runtime.NewScheme()
	if err := corev1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add corev1 to scheme. %v", err)
	}
	if err := ocsv1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add ocsv1 to scheme. %v", err)
	}
	if err := cephv1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add cephv1 to scheme. %v", err)
	}
	if err := storagev1.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add storagev1 to scheme. %v", err)
	}
	if err := noobaaapis.AddToScheme(scheme); err != nil {
		klog.Exitf("failed to add noobaaapis to scheme. %v", err)
	}

	klog.Info("Setting up cached client for the ux backend server")
	clientConfig, err := ctrlconfig.GetConfig()
	if err != nil {
		klog.Exitf("failed to get cached client config: %v", err)
	}

	cache, err := ctrlcache.New(clientConfig, ctrlcache.Options{
		Scheme: scheme,
		DefaultNamespaces: map[string]ctrlcache.Config{
			namespace:                    {},
			"openshift-storage-extended": {},
		},
	})
	if err != nil {
		klog.Exitf("failed to create cache: %v", err)
	}

	if err := cache.IndexField(ctx, &storagev1.StorageClass{}, "provisioner", func(obj client.Object) []string {
		sc := obj.(*storagev1.StorageClass)
		return []string{sc.Provisioner}
	}); err != nil {
		klog.Exitf("failed to add provisioner index for StorageClasses: %v", err)
	}

	if err := cache.IndexField(ctx, &nbv1alpha1.ObjectBucket{}, "bucketName", func(obj client.Object) []string {
		ob := obj.(*nbv1alpha1.ObjectBucket)
		if ob.Spec.Endpoint != nil {
			return []string{ob.Spec.Endpoint.BucketName}
		}
		return []string{}
	}); err != nil {
		klog.Exitf("failed to add bucketName index for ObjectBuckets: %v", err)
	}

	cl, err := client.New(clientConfig, client.Options{
		Scheme: scheme,
		Cache: &client.CacheOptions{
			Reader: cache,
		},
	})
	if err != nil {
		klog.Exitf("failed to create cached kube client: %v", err)
	}

	klog.Info("Starting cache for ux backend server")
	go func() {
		if err := cache.Start(ctx); err != nil {
			klog.Errorf("failed to start cache: %v", err)
			os.Exit(1)
		}
	}()

	if !cache.WaitForCacheSync(ctx) {
		panic("cache did not sync")
	}

	// Authenticated + Authorized endpoints (require both authentication and authorization)

	http.HandleFunc("/onboarding/peer-tokens", func(w http.ResponseWriter, r *http.Request) {
		peertokens.HandleMessage(w, r, config.tokenLifetimeInHours, cl, namespace)
	})

	http.HandleFunc("/expandstorage", func(w http.ResponseWriter, r *http.Request) {
		expandstorage.HandleMessage(w, r, cl, namespace)
	})

	// Authenticated endpoints (require authentication but not authorization)

	http.HandleFunc("/info/featureflags", func(w http.ResponseWriter, r *http.Request) {
		featureflags.HandleMessage(w, r, cl, namespace)
	})

	http.HandleFunc("/info/bucket/{name}", func(w http.ResponseWriter, r *http.Request) {
		bucket.HandleMessage(w, r, cl)
	})

	http.HandleFunc("/info/storages", func(w http.ResponseWriter, r *http.Request) {
		storages.HandleMessage(w, r, cl, namespace)
	})

	klog.Info("ux backend server listening on port ", config.listenPort)

	addr := fmt.Sprintf("%s%d", ":", config.listenPort)
	if config.tlsEnabled {
		klog.Info("Server configured to run with TLS")
		err = http.ListenAndServeTLS(addr,
			"/etc/tls/private/tls.crt",
			"/etc/tls/private/tls.key",
			nil,
		)
	} else {
		klog.Info("Server configured to run without TLS")
		err = http.ListenAndServe(addr, nil)
	}
	log.Fatal(err)

}
