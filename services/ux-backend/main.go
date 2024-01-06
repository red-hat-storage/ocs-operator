package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"

	"k8s.io/klog/v2"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers/onboardingtokens"
)

type serverConfig struct {
	listenPort           int
	tokenLifetimeInHours int
	tlsEnabled           bool
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
		return nil, fmt.Errorf("malformed user-defined Token lifetime %s, %v", tokenLifetimeInHoursAsString, err)
	}

	klog.Infof("generated tokens will be valid for %d hours", config.tokenLifetimeInHours)

	defaultListeningPort := 8080
	listenPortAsString := os.Getenv("UX_BACKEND_PORT")
	if listenPortAsString == "" {
		klog.Infof("No user-defined server listening port provided, defaulting to %d ", defaultListeningPort)
		config.listenPort = defaultListeningPort
	} else if config.listenPort, err = strconv.Atoi(listenPortAsString); err != nil {
		return nil, fmt.Errorf("malformed user-defined listening port %s, %v", listenPortAsString, err)
	}

	defaultTLSEnabled := false
	tlsEnabledAsString := os.Getenv("TLS_ENABLED")
	if tlsEnabledAsString == "" {
		klog.Infof("No user-defined TLS enabled value provided, defaulting to %t ", defaultTLSEnabled)
		config.tlsEnabled = defaultTLSEnabled
	} else if config.tlsEnabled, err = strconv.ParseBool(tlsEnabledAsString); err != nil {
		return nil, fmt.Errorf("malformed user-defined TLS Enabled value %s, %v", tlsEnabledAsString, err)
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
	http.HandleFunc("/onboarding-tokens", func(w http.ResponseWriter, r *http.Request) {
		onboardingtokens.HandleMessage(w, r, config.tokenLifetimeInHours)
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
