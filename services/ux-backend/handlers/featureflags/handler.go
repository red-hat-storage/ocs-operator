package featureflags

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	FlagNoobaa string = "noobaa"
)

func HandleMessage(w http.ResponseWriter, r *http.Request, client client.Client, namespace string) {
	switch r.Method {
	case "GET":
		handleGet(w, r, client, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

// request format (comma-separated flags): /auth/featureflags?flags=noobaa,rgw
func handleGet(w http.ResponseWriter, r *http.Request, client client.Client, namespace string) {
	flagsParam := r.URL.Query().Get("flags")
	if flagsParam == "" {
		klog.Errorf("flags parameter is required")
		http.Error(w, "flags parameter is required", http.StatusBadRequest)
		return
	}

	flagNames := strings.Split(flagsParam, ",")
	for i, flag := range flagNames {
		flagNames[i] = strings.TrimSpace(flag)
	}

	type flagResult struct {
		Value bool   `json:"value"`
		Error string `json:"error,omitempty"`
	}

	result := make(map[string]flagResult)
	for _, flagName := range flagNames {
		switch flagName {
		case FlagNoobaa:
			value, err := checkNoobaaFlag(client, namespace)
			if err != nil {
				klog.Errorf("failed to check noobaa flag: %v", err)
				result[flagName] = flagResult{Value: false, Error: err.Error()}
			} else {
				result[flagName] = flagResult{Value: value}
			}
		default:
			result[flagName] = flagResult{Value: false, Error: "unsupported flag"}
		}
	}

	w.Header().Set("Content-Type", "application/json")

	allFlagsFailed := true
	for _, res := range result {
		if res.Error == "" {
			allFlagsFailed = false
			break
		}
	}

	// if all flags failed, returning "500" as response
	// if some flags succeeded, returning "200" with partial results and error details
	if allFlagsFailed && len(result) > 0 {
		w.WriteHeader(http.StatusInternalServerError)
	} else {
		w.WriteHeader(http.StatusOK)
	}

	if err := json.NewEncoder(w).Encode(result); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func checkNoobaaFlag(client client.Client, namespace string) (bool, error) {
	noobaaExists, err := checkNoobaaCRExists(client, namespace)
	if err != nil {
		return false, fmt.Errorf("failed to check NooBaa CR: %w", err)
	}
	if !noobaaExists {
		return false, nil
	}

	storageClassExists, err := checkNoobaaStorageClassExists(client)
	if err != nil {
		return false, fmt.Errorf("failed to check NooBaa StorageClass: %w", err)
	}

	return storageClassExists, nil
}

func checkNoobaaCRExists(client client.Client, namespace string) (bool, error) {
	klog.Info("Checking NooBaa CR existence in namespace:", namespace)

	// ToDo: Implement NooBaa CR check
	// Import the noobaa.io/v1alpha1 API
	// Check for NooBaa resources in the namespace

	return false, nil // placeholder
}

func checkNoobaaStorageClassExists(client client.Client) (bool, error) {
	klog.Info("Checking for NooBaa StorageClass existence")
	
	// ToDo: Implement StorageClass check
	// List StorageClasses and check if any
	// have provisioner ending with "noobaa.io/obc"
	
	return false, nil // placeholder
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only GET method should be used for this endpoint %s", r.URL.Path)
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "GET")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method: %s", r.Method))); err != nil {
		klog.Errorf("failed to write data to response writer: %v", err)
	}
}
