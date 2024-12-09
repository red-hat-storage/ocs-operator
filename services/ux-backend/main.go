package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/controllers/util"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/expandstorage"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/onboarding/clienttokens"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/onboarding/peertokens"
	cephv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	storagev1 "k8s.io/api/storage/v1"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"

	"k8s.io/klog/v2"
)

type serverConfig struct {
	listenPort           int
	tokenLifetimeInHours int
	tlsEnabled           bool
}

func readEnvVar[T any](envVarName string, defaultValue T, parser func(str string) (T, error)) (T, error) {
	if str := os.Getenv(envVarName); str == "" {
		klog.Infof("no user-defined %s provided, defaulting to %v", envVarName, defaultValue)
		return defaultValue, nil
	} else if value, err := parser(str); err != nil {
		return *new(T), fmt.Errorf("malformed user-defined %s value %s: %v", envVarName, str, err)
	} else {
		return value, nil
	}
}

func loadAndValidateServerConfig() (*serverConfig, error) {
	var config serverConfig
	var err error

	config.tokenLifetimeInHours, err = readEnvVar("ONBOARDING_TOKEN_LIFETIME", 48, strconv.Atoi)
	if err != nil {
		return nil, err
	}

	config.listenPort, err = readEnvVar("UX_BACKEND_PORT", 8080, strconv.Atoi)
	if err != nil {
		return nil, err
	}

	config.tlsEnabled, err = readEnvVar("TLS_ENABLED", false, strconv.ParseBool)
	if err != nil {
		return nil, err
	}

	return &config, nil
}

func main() {

	klog.Info("Starting ux backend server")

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

	cl, err := util.NewK8sClient(scheme)
	if err != nil {
		klog.Exitf("failed to create kube client: %v", err)
	}

	// TODO: remove '/onboarding-tokens' in the future
	http.HandleFunc("/onboarding-tokens", func(w http.ResponseWriter, r *http.Request) {
		// Set the Deprecation header
		w.Header().Set("Deprecation", "true") // Standard "Deprecation" header
		w.Header().Set("Link", "/onboarding/client-tokens; rel=\"alternate\"")
		clienttokens.HandleMessage(w, r, config.tokenLifetimeInHours, cl, namespace)
	})
	http.HandleFunc("/onboarding/client-tokens", func(w http.ResponseWriter, r *http.Request) {
		clienttokens.HandleMessage(w, r, config.tokenLifetimeInHours, cl, namespace)
	})
	http.HandleFunc("/onboarding/peer-tokens", func(w http.ResponseWriter, r *http.Request) {
		peertokens.HandleMessage(w, r, config.tokenLifetimeInHours, cl, namespace)
	})

	http.HandleFunc("/expandstorage", func(w http.ResponseWriter, r *http.Request) {
		expandstorage.HandleMessage(w, r, cl, namespace)
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
