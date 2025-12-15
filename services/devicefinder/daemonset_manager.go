package devicefinder

import (
	"context"
	"fmt"
	"os"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/assets"

	corev1 "k8s.io/api/core/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeviceFinderDiscovery = "devicefinder-discovery"
)

// CreateDeviceFinderDaemonSet creates a daemonset for device discovery
func CreateOrUpdateDeviceFinderDaemonSet(ctx context.Context, c client.Client, namespace string, nodeSelector *corev1.NodeSelector, tolerations []corev1.Toleration) error {
	// Get device finder image from environment or use default
	deviceFinderImage := os.Getenv("DEVICEFINDER_IMAGE")
	if deviceFinderImage == "" {
		return fmt.Errorf("DEVICEFINDER_IMAGE environment variable is not set")
	}

	// Create DaemonSet object directly
	daemonSet := assets.NewDeviceFinderDaemonSet(namespace, deviceFinderImage)

	_, err := ctrl.CreateOrUpdate(ctx, c, daemonSet, func() error {
		desiredSpec := daemonSet.Spec.DeepCopy()

		// Apply node selector if provided
		if nodeSelector != nil {
			if desiredSpec.Template.Spec.Affinity == nil {
				desiredSpec.Template.Spec.Affinity = &corev1.Affinity{}
			}
			if desiredSpec.Template.Spec.Affinity.NodeAffinity == nil {
				desiredSpec.Template.Spec.Affinity.NodeAffinity = &corev1.NodeAffinity{}
			}
			desiredSpec.Template.Spec.Affinity.NodeAffinity.RequiredDuringSchedulingIgnoredDuringExecution = nodeSelector
		}

		// Apply tolerations if provided
		if len(tolerations) > 0 {
			desiredSpec.Template.Spec.Tolerations = tolerations
		}

		daemonSet.Spec = *desiredSpec
		return nil
	})
	if err != nil {
		return fmt.Errorf("failed to create or update daemonset: %w", err)
	}

	return nil
}
