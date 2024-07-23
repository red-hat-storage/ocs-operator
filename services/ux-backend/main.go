package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	v1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/onboardingtokens"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

type serverConfig struct {
	client.Client
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

	cl, err := newClient()
	if err != nil {
		klog.Exitf("failed to create client: %v", err)
	}

	http.HandleFunc("/onboarding-tokens", func(w http.ResponseWriter, r *http.Request) {
		onboardingtokens.HandleMessage(w, r, config.tokenLifetimeInHours, cl)
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

func newClient() (client.Client, error) {
	klog.Info("Setting up k8s client")
	scheme := runtime.NewScheme()
	if err := v1.AddToScheme(scheme); err != nil {
		return nil, err
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return nil, err
	}

	config, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	k8sClient, err := client.New(config, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}

	return k8sClient, nil
}
