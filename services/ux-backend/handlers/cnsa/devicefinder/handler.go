package devicefinder

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder"
	"github.com/red-hat-storage/ocs-operator/v4/services/ux-backend/handlers"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// DiscoveryRequest represents the request body for device discovery
type DiscoveryRequest struct {
	NodeSelector *corev1.NodeSelector `json:"nodeSelector,omitempty"`
	Tolerations  []corev1.Toleration  `json:"tolerations,omitempty"`
}

type DiscoveryResponse struct {
	Devices map[string]DiscoveryResult `json:"devices,omitempty"`
}

// DiscoveryResult represents the result of device discovery for a node
type DiscoveryResult struct {
	DiscoveredDevices []DiscoveredDevice `json:"discoveredDevices"`
}

// DiscoveredDevice shows the list of discovered devices with their properties
type DiscoveredDevice struct {
	Path string `json:"path"`
	Type string `json:"type"`
	Size int64  `json:"size"`
	WWN  string `json:"WWN"`
}

func HandleMessage(w http.ResponseWriter, r *http.Request, cl client.Client, namespace string) {
	switch r.Method {
	case "POST":
		handleInput(w, r, cl)
	case "PUT":
		handleInput(w, r, cl)
	case "GET":
		handleGet(w, r, cl)
	default:
		handleUnsupportedMethod(w, r)
	}
}

func handleInput(w http.ResponseWriter, r *http.Request, cl client.Client) {
	// Parse request body
	var req DiscoveryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		klog.Errorf("failed to decode request body: %v", err)
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	targetNamespace := os.Getenv("POD_NAMESPACE")
	if targetNamespace == "" {
		klog.Errorf("POD_NAMESPACE environment variable is not set")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Create device finder daemonset
	err := devicefinder.CreateOrUpdateDeviceFinderDaemonSet(r.Context(), cl, targetNamespace, req.NodeSelector, req.Tolerations)
	if err != nil {
		klog.Errorf("failed to create device finder daemonset: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode("Device discovery started"); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func handleGet(w http.ResponseWriter, r *http.Request, cl client.Client) {

	targetNamespace := os.Getenv("POD_NAMESPACE")
	if targetNamespace == "" {
		klog.Errorf("POD_NAMESPACE environment variable is not set")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// List all configmaps with devicefinder-result- prefix
	configMapList := &corev1.ConfigMapList{}
	err := cl.List(r.Context(), configMapList, client.InNamespace(targetNamespace), client.MatchingLabels{"app": "devicefinder"})
	if err != nil {
		klog.Errorf("failed to list device configmaps: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	podList := &corev1.PodList{}
	err = cl.List(r.Context(), podList, client.InNamespace(targetNamespace), client.MatchingLabels{"app": "devicefinder-discovery"})
	if err != nil {
		klog.Errorf("failed to list device pods: %v", err)
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	if len(podList.Items) != len(configMapList.Items) {
		klog.Errorf("number of devicefinder pods and configmaps do not match: %d pods, %d configmaps", len(podList.Items), len(configMapList.Items))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	// Parse configmaps and extract device information
	devices := make(map[string]DiscoveryResult)
	for _, configMap := range configMapList.Items {
		// Extract node name from configmap name (format: devicefinder-result-<node-name>)
		if !strings.HasPrefix(configMap.Name, "devicefinder-result-") {
			klog.Errorf("configmap name %s is not a valid devicefinder configmap", configMap.Name)
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		nodeName := configMap.Name[len("devicefinder-result-"):]

		// Parse discovered devices from configmap data
		var discoveredDevices []DiscoveredDevice
		if devicesJSON, exists := configMap.Data["discovered-devices"]; exists {
			if err := json.Unmarshal([]byte(devicesJSON), &discoveredDevices); err != nil {
				klog.Errorf("failed to unmarshal discovered devices for node %s: %v", nodeName, err)
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
		}

		devices[nodeName] = DiscoveryResult{
			DiscoveredDevices: discoveredDevices,
		}
	}

	response := DiscoveryResponse{
		Devices: devices,
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(response); err != nil {
		klog.Errorf("failed to encode response: %v", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
		return
	}
}

func handleUnsupportedMethod(w http.ResponseWriter, r *http.Request) {
	klog.Infof("Only POST, PUT and GET and method should be used to send data to this endpoint %s", r.URL.Path)
	w.Header().Set("Content-Type", handlers.ContentTypeTextPlain)
	w.Header().Set("Allow", "POST, PUT, GET")
	w.WriteHeader(http.StatusMethodNotAllowed)

	if _, err := w.Write([]byte(fmt.Sprintf("Unsupported method : %s", r.Method))); err != nil {
		klog.Errorf("failed write data to response writer: %v", err)
	}
}
