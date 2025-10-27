package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/types"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

const (
	configMapNamePrefix = "devicefinder-result-"
)

// updateConfigMap creates or updates the ConfigMap with discovered devices
func (discovery *DeviceDiscovery) updateConfigMap() error {
	nodeName := os.Getenv("MY_NODE_NAME")
	namespace := os.Getenv("WATCH_NAMESPACE")

	if nodeName == "" || namespace == "" {
		return fmt.Errorf("missing required environment variables: MY_NODE_NAME or WATCH_NAMESPACE")
	}

	// Create Kubernetes client
	config, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("failed to get in-cluster config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create discovery result
	result := types.DiscoveryResult{
		DiscoveredTimeStamp: time.Now().UTC().Format(time.RFC3339),
		DiscoveredDevices:   discovery.disks,
	}

	// Marshal devices to JSON
	devicesJSON, err := json.MarshalIndent(result.DiscoveredDevices, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal discovered devices: %w", err)
	}

	// Create ConfigMap data
	configMapData := map[string]string{
		"discovered-devices":  string(devicesJSON),
		"discoveredTimeStamp": result.DiscoveredTimeStamp,
	}

	configMapName := configMapNamePrefix + nodeName

	// Check if ConfigMap exists
	existingConfigMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(context.TODO(), configMapName, metav1.GetOptions{})
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to get existing ConfigMap: %w", err)
		}

		// ConfigMap doesn't exist, create it
		configMap := &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      configMapName,
				Namespace: namespace,
				Labels: map[string]string{
					"app":  "devicefinder",
					"node": nodeName,
				},
			},
			Data: configMapData,
		}

		_, err = clientset.CoreV1().ConfigMaps(namespace).Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create ConfigMap: %w", err)
		}

		klog.Infof("Created ConfigMap %s in namespace %s", configMapName, namespace)
		return nil
	}

	// ConfigMap exists, update it
	existingConfigMap.Data = configMapData
	_, err = clientset.CoreV1().ConfigMaps(namespace).Update(context.TODO(), existingConfigMap, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update ConfigMap: %w", err)
	}

	klog.Infof("Updated ConfigMap %s in namespace %s", configMapName, namespace)
	return nil
}
