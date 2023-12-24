package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	"k8s.io/klog/v2"
)

type serverConfig struct {
	listenPort           int
	tokenLifetimeInHours int
}

func loadAndValidateServerConfig() (*serverConfig, error) {
	var config serverConfig

	var err error
	defaultTokenLifetimeInHours := 48
	tokenLifetimeInHoursAsString := os.Getenv("ONBOARDING_TOKEN_LIFETIME")
	if tokenLifetimeInHoursAsString == "" {
		klog.Infof("No user-defined token lifetime provided, defaulting to %d ", defaultTokenLifetimeInHours)
		config.tokenLifetimeInHours = defaultTokenLifetimeInHours
	} else if config.tokenLifetimeInHours, err = strconv.Atoi(tokenLifetimeInHoursAsString); err != nil {
		return nil, fmt.Errorf("Malformed user-defined Token lifetime: %s. shutting down: %v", tokenLifetimeInHoursAsString, err)
	}

	klog.Infof("generated tokens will be valid for %d hours", config.tokenLifetimeInHours)

	defaultListeningPort := 8080
	listenPortAsString := os.Getenv("UX_BACKEND_PORT")
	if listenPortAsString == "" {
		klog.Infof("No user-defined server listening port provided, defaulting to %d ", defaultListeningPort)
		config.listenPort = defaultListeningPort
	} else if config.listenPort, err = strconv.Atoi(listenPortAsString); err != nil {
		return nil, fmt.Errorf("Malformed user-defined listening port:  %s. shutting down: %v", listenPortAsString, err)
	}

	return &config, nil
}

func main() {

	klog.Info("Starting ux backend server")

	config, err := loadAndValidateServerConfig()
	if err != nil {
		klog.Errorf("failed to load server config: %v", err)
		os.Exit(-1)
	}
	http.HandleFunc("/onboarding-tokens", func(w http.ResponseWriter, r *http.Request) {
		handlers.OnboardingTokensHandler(w, r, config.tokenLifetimeInHours)

	})

	klog.Info("ux backend server listening on port ", config.listenPort)

	log.Fatal(http.ListenAndServeTLS(
		fmt.Sprintf("%s%d", ":", config.listenPort),
		"/etc/tls/private/tls.crt",
		"/etc/tls/private/tls.key",
		nil,
	))

}
