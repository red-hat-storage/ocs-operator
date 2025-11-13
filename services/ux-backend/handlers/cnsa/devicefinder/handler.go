package devicefinder

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoveryRequest represents the request body for device discovery
type DiscoveryRequest struct {
	Namespace    string               `json:"namespace"`
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration  `json:"tolerations,omitempty"`
}

// DiscoveryResponse represents the response for device discovery
type DiscoveryResponse struct {
	Message string                     `json:"message"`
	Success bool                       `json:"success"`
	Devices map[string]DiscoveryResult `json:"devices,omitempty"`
}

// DiscoveryResult represents the result of device discovery for a node
type DiscoveryResult struct {
	DiscoveredTimeStamp string             `json:"discoveredTimeStamp,omitempty"`
	DiscoveredDevices   []DiscoveredDevice `json:"discoveredDevices"`
}

// DiscoveredDevice shows the list of discovered devices with their properties
type DiscoveredDevice struct {
	DeviceID string `json:"deviceID"`
	Path     string `json:"path"`
	Model    string `json:"model"`
	Type     string `json:"type"`
	Vendor   string `json:"vendor"`
	Size     int64  `json:"size"`
	WWN      string `json:"WWN"`
}

func HandleMessage(w http.ResponseWriter, r *http.Request, cl client.Client, namespace string) {
	switch r.Method {
	case "POST":
		handlePost(w, r, cl, namespace)
	case "GET":
		handleGet(w, r, cl, namespace)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handlePost(w http.ResponseWriter, r *http.Request, cl client.Client, defaultNamespace string) {
	// Parse request body
	var req DiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		klog.Errorf("failed to decode request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Use namespace from request or default
	targetNamespace := req.Namespace
	if targetNamespace == "" {
		targetNamespace = defaultNamespace
	}

	// Create device finder daemonset
	err := devicefinder.CreateDeviceFinderDaemonSet(r.Context(), cl, targetNamespace, req.NodeSelector, req.Tolerations)
	if err != nil {
		klog.Errorf("failed to create device finder daemonset: %v", err)
		response := DiscoveryResponse{
			Message: fmt.Sprintf("Failed to create device finder daemonset: %v", err),
			Success: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Return success response
	response := DiscoveryResponse{
		Message: "Device finder daemonset created successfully",
		Success: true,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func handleGet(w http.ResponseWriter, r *http.Request, cl client.Client, defaultNamespace string) {
	// Parse request body
	var req DiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		klog.Errorf("failed to decode request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Use namespace from request or default
	targetNamespace := req.Namespace
	if targetNamespace == "" {
		targetNamespace = defaultNamespace
	}

	// List all configmaps with devicefinder-result- prefix
	configMapList := &corev1.ConfigMapList{}
	err := cl.List(r.Context(), configMapList, client.InNamespace(targetNamespace), client.MatchingLabels{"app": "devicefinder"})
	if err != nil {
		klog.Errorf("failed to list device configmaps: %v", err)
		response := DiscoveryResponse{
			Message: fmt.Sprintf("Failed to list device configmaps: %v", err),
			Success: false,
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(response)
		return
	}

	// Parse configmaps and extract device information
	devices := make(map[string]DiscoveryResult)
	for _, configMap := range configMapList.Items {
		// Extract node name from configmap name (format: devicefinder-result-<node-name>)
		if len(configMap.Name) <= len("devicefinder-result-") {
			continue
		}
		nodeName := configMap.Name[len("devicefinder-result-"):]

		// Parse discovered devices from configmap data
		var discoveredDevices []DiscoveredDevice
		if devicesJSON, exists := configMap.Data["discovered-devices"]; exists {
			if err := json.Unmarshal([]byte(devicesJSON), &discoveredDevices); err != nil {
				klog.Errorf("failed to unmarshal discovered devices for node %s: %v", nodeName, err)
				continue
			}
		}

		// Get timestamp
		timestamp := configMap.Data["discoveredTimeStamp"]

		devices[nodeName] = DiscoveryResult{
			DiscoveredTimeStamp: timestamp,
			DiscoveredDevices:   discoveredDevices,
		}
	}

	// Return success response with device data
	response := DiscoveryResponse{
		Message: fmt.Sprintf("Found device configmaps for %d nodes", len(devices)),
		Success: true,
		Devices: devices,
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	json.NewEncoder(w).Encode(response)
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only POST method should be used to send data to this endpoint %s", r.URL.Path)
	w.WriteHeader(http.StatusMethodNotAllowed)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST")

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
