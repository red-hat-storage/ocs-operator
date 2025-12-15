package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/types"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	apimachinerytypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
)

const (
	configMapNamePrefix = "devicefinder-result-"
)

// updateConfigMap creates or updates the ConfigMap with discovered devices
func (discovery *DeviceDiscovery) updateConfigMap() error {
	nodeName := os.Getenv("NODE_NAME")
	namespace := os.Getenv("POD_NAMESPACE")
	podUID := os.Getenv("POD_UID")
	podName := os.Getenv("POD_NAME")

	if nodeName == "" || namespace == "" || podUID == "" || podName == "" {
		return fmt.Errorf("missing required environment variables: NODE_NAME or POD_NAMESPACE or POD_UID or POD_NAME")
	}

	// Create discovery result
	result := types.DiscoveryResult{
		DiscoveredDevices: discovery.disks,
	}

	// Marshal devices to JSON
	devicesJSON, err := json.MarshalIndent(result.DiscoveredDevices, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal discovered devices: %w", err)
	}

	// Create ConfigMap data
	configMapData := map[string]string{
		"discovered-devices": string(devicesJSON),
	}

	configMapName := configMapNamePrefix + nodeName
	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      configMapName,
			Namespace: namespace,
			Labels: map[string]string{
				"app":  "devicefinder",
				"node": nodeName,
			},
			OwnerReferences: []metav1.OwnerReference{
				{
					APIVersion: "v1",
					Kind:       "Pod",
					Name:       podName,
					UID:        apimachinerytypes.UID(podUID),
				},
			},
		},
	}

	_, err = ctrl.CreateOrUpdate(context.TODO(), discovery.kubeClient, configMap, func() error {
		configMap.Data = configMapData
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update ConfigMap: %w", err)
	}

	klog.Infof("Updated ConfigMap %s in namespace %s", configMapName, namespace)
	return nil
}
