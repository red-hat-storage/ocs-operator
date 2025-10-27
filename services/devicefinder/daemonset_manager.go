package devicefinder

import (
	"context"
	"fmt"
	"os"

	"github.com/red-hat-storage/ocs-operator/v4/services/devicefinder/assets"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/klog/v2"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	DeviceFinderDiscovery    = "devicefinder-discovery"
	defaultDeviceFinderImage = "quay.io/red-hat-storage/ocs-operator:latest"
)

// CreateDeviceFinderDaemonSet creates a daemonset for device discovery
func CreateDeviceFinderDaemonSet(ctx context.Context, c client.Client, namespace string, nodeSelector *corev1.NodeSelector, tolerations []corev1.Toleration) error {
	// Get device finder image from environment or use default
	deviceFinderImage := os.Getenv("RELATED_IMAGE_DEVICEFINDER")
	if deviceFinderImage == "" {
		deviceFinderImage = defaultDeviceFinderImage
	}

	// Get priority class from environment
	priorityClassName := os.Getenv("PRIORITY_CLASS_NAME")

	// Replace template variables
	pairs := []string{
		"${OBJECT_NAMESPACE}", namespace,
		"${CONTAINER_IMAGE}", deviceFinderImage,
		"${PRIORITY_CLASS_NAME}", priorityClassName,
	}

	daemonSetYAML, err := assets.ReadFileAndReplace(assets.DeviceFinderDaemonSetTemplate, pairs)
	if err != nil {
		return fmt.Errorf("failed to process daemonset template: %w", err)
	}

	// Decode YAML to DaemonSet object
	scheme := runtime.NewScheme()
	if err := appsv1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add appsv1 to scheme: %w", err)
	}
	if err := corev1.AddToScheme(scheme); err != nil {
		return fmt.Errorf("failed to add corev1 to scheme: %w", err)
	}

	codecs := serializer.NewCodecFactory(scheme)
	obj, err := runtime.Decode(codecs.UniversalDecoder(appsv1.SchemeGroupVersion), daemonSetYAML)
	if err != nil {
		return fmt.Errorf("failed to decode daemonset YAML: %w", err)
	}

	daemonSet := obj.(*appsv1.DaemonSet)

	// Apply node selector if provided
	if nodeSelector != nil {
		daemonSet.Spec.Template.Spec.Affinity = &corev1.Affinity{
			NodeAffinity: &corev1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: nodeSelector,
			},
		}
	}

	// Apply tolerations if provided
	if len(tolerations) > 0 {
		daemonSet.Spec.Template.Spec.Tolerations = tolerations
	}

	// Check if daemonset already exists
	existingDS := &appsv1.DaemonSet{}
	err = c.Get(ctx, client.ObjectKey{Name: DeviceFinderDiscovery, Namespace: namespace}, existingDS)
	if err != nil {
		if !errors.IsNotFound(err) {
			return fmt.Errorf("failed to check existing daemonset: %w", err)
		}
		// DaemonSet doesn't exist, create it
		err = c.Create(ctx, daemonSet)
		if err != nil {
			return fmt.Errorf("failed to create daemonset: %w", err)
		}
		klog.Info("Created device finder daemonset")
	} else {
		// DaemonSet exists, update it
		existingDS.Spec = daemonSet.Spec
		err = c.Update(ctx, existingDS)
		if err != nil {
			return fmt.Errorf("failed to update daemonset: %w", err)
		}
		klog.Info("Updated device finder daemonset")
	}

	return nil
}

// DeleteDeviceFinderDaemonSet deletes the device finder daemonset
func DeleteDeviceFinderDaemonSet(ctx context.Context, c client.Client, namespace string) error {
	daemonSet := &appsv1.DaemonSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      DeviceFinderDiscovery,
			Namespace: namespace,
		},
	}

	err := c.Delete(ctx, daemonSet)
	if err != nil {
		if errors.IsNotFound(err) {
			klog.Info("Device finder daemonset not found, nothing to delete")
			return nil
		}
		return fmt.Errorf("failed to delete daemonset: %w", err)
	}

	klog.Info("Deleted device finder daemonset")
	return nil
}
