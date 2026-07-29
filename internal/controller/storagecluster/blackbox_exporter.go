/*
Copyright 2025 Red Hat OpenShift Container Storage.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package storagecluster

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	securityv1 "github.com/openshift/api/security/v1"
	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	ocsv1 "github.com/red-hat-storage/ocs-operator/api/v4/v1"
	"github.com/red-hat-storage/ocs-operator/v4/pkg/defaults"
	"go.uber.org/multierr"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
)

const (
	blackboxExporterName   = "odf-blackbox-exporter"
	blackboxServiceAccount = "odf-blackbox-exporter"
	blackboxSCCName        = "odf-blackbox-scc"
	blackboxConfigMapName  = "odf-blackbox-exporter-config"
	portBlackboxHTTP       = "http"
	blackboxScrapeInterval = "30s"
	blackboxPortNumber     = 9115
)

var blackboxExporterLabels = map[string]string{
	"app": blackboxExporterName,
}

type MultusNetStatus struct {
	Name      string   `json:"name"`
	Interface string   `json:"interface"`
	IPs       []string `json:"ips"`
}

type NetworkAttachment struct {
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Interface string `json:"interface,omitempty"`
}

// deployBlackboxExporter ensures the Blackbox Exporter is deployed by default
func (r *StorageClusterReconciler) deployBlackboxExporter(ctx context.Context, instance *ocsv1.StorageCluster) error {
	if instance.Spec.Monitoring != nil && instance.Spec.Monitoring.DisableBlackboxExporter {
		return r.deleteBlackboxExporter(ctx, instance)
	}

	// Get the Ceph cluster network configuration from OSD pods
	cephClusterNetwork, err := r.getCephClusterNetwork(ctx, instance.Namespace)
	if err != nil {
		r.Log.Error(err, "Failed to get Ceph cluster network")
		return err
	}

	// Build Multus annotation for Blackbox (attach to same networks as Ceph cluster)
	var podAnnotations map[string]string
	multusValue := buildMultusAnnotation(cephClusterNetwork, instance.Namespace)
	if multusValue != "" {
		podAnnotations = map[string]string{
			"k8s.v1.cni.cncf.io/networks": multusValue,
		}
		r.Log.Info("Attaching Blackbox to Ceph cluster network", "networks", cephClusterNetwork)
	}

	if err := r.createBlackboxServiceAccount(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create serviceaccount for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxSCC(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create SCC for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxConfigMap(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create configmap for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxDeployment(ctx, instance, podAnnotations); err != nil {
		r.Log.Error(err, "unable to create deployment for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxService(ctx, instance); err != nil {
		r.Log.Error(err, "unable to create service for blackbox metrics exporter")
		return err
	}
	if err := r.createBlackboxProbe(ctx, instance, cephClusterNetwork); err != nil {
		r.Log.Error(err, "unable to create probe for blackbox metrics exporter")
		return err
	}
	return nil
}

// deleteBlackboxExporter removes all Blackbox Exporter components
func (r *StorageClusterReconciler) deleteBlackboxExporter(ctx context.Context, instance *ocsv1.StorageCluster) error {
	var finalErr error

	// Namespaced resources to delete using cached client
	resources := []client.Object{
		&monitoringv1.Probe{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&corev1.Service{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: blackboxExporterName, Namespace: instance.Namespace}},
		&corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: blackboxConfigMapName, Namespace: instance.Namespace}},
		&corev1.ServiceAccount{ObjectMeta: metav1.ObjectMeta{Name: blackboxServiceAccount, Namespace: instance.Namespace}},
	}

	for _, obj := range resources {
		err := r.Delete(ctx, obj)
		if err != nil && !apierrors.IsNotFound(err) {
			r.Log.Error(err, "Failed to delete Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
			multierr.AppendInto(&finalErr, err)
		} else if err == nil {
			r.Log.Info("Deleted Blackbox resource", "Kind", fmt.Sprintf("%T", obj), "Name", obj.GetName())
		}
	}

	// Delete cluster-scoped SCC using direct API client
	err := r.SecurityClient.SecurityContextConstraints().Delete(ctx, blackboxSCCName, metav1.DeleteOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		r.Log.Error(err, "Failed to delete Blackbox SCC", "SCC", blackboxSCCName)
		multierr.AppendInto(&finalErr, err)
	} else if err == nil {
		r.Log.Info("Deleted Blackbox SCC", "SCC", blackboxSCCName)
	}

	return finalErr
}

// createBlackboxServiceAccount creates or updates the ServiceAccount
func (r *StorageClusterReconciler) createBlackboxServiceAccount(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxServiceAccount,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
			OwnerReferences: []metav1.OwnerReference{{
				APIVersion: instance.APIVersion,
				Kind:       instance.Kind,
				Name:       instance.Name,
				UID:        instance.UID,
			}},
		},
	}
	actual := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	err := r.Get(ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return r.Create(ctx, desired)
		}
		return err
	}

	// If ServiceAccount exists and Labels and OwnerReferences are correct, we don't need to update anything.
	if equality.Semantic.DeepEqual(desired.Labels, actual.Labels) &&
		equality.Semantic.DeepEqual(desired.OwnerReferences, actual.OwnerReferences) {
		return nil
	}
	// If ServiceAccount exists but Labels and/or OwnerReferences are incorrect, we need to update it.
	actual.Labels = desired.Labels
	actual.OwnerReferences = desired.OwnerReferences
	err = r.Update(ctx, actual)
	return err

}

// createBlackboxSCC creates or updates the SCC
func (r *StorageClusterReconciler) createBlackboxSCC(ctx context.Context, instance *ocsv1.StorageCluster) error {
	desired := &securityv1.SecurityContextConstraints{
		ObjectMeta: metav1.ObjectMeta{
			Name: blackboxSCCName,
			Labels: map[string]string{
				nameLabel: blackboxExporterName,
			},
		},
		AllowPrivilegedContainer: false,
		RunAsUser: securityv1.RunAsUserStrategyOptions{
			Type: securityv1.RunAsUserStrategyMustRunAsRange,
		},
		SELinuxContext: securityv1.SELinuxContextStrategyOptions{
			Type: securityv1.SELinuxStrategyMustRunAs,
		},
		FSGroup: securityv1.FSGroupStrategyOptions{
			Type: securityv1.FSGroupStrategyMustRunAs,
		},
		SupplementalGroups: securityv1.SupplementalGroupsStrategyOptions{
			Type: securityv1.SupplementalGroupsStrategyRunAsAny,
		},
		Volumes: []securityv1.FSType{
			securityv1.FSTypeConfigMap,
			securityv1.FSTypeSecret,
			securityv1.FSTypeDownwardAPI,
			securityv1.FSTypeEmptyDir,
		},
		Users: []string{
			fmt.Sprintf("system:serviceaccount:%s:%s", instance.Namespace, blackboxServiceAccount),
		},
		AllowedCapabilities:  []corev1.Capability{"NET_RAW"},
		AllowedUnsafeSysctls: []string{"net.ipv4.ping_group_range"},
		SeccompProfiles:      []string{"runtime/default"},
		Priority:             ptr.To[int32](10),
	}
	actual, err := r.SecurityClient.SecurityContextConstraints().Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			if _, err := r.SecurityClient.SecurityContextConstraints().Create(ctx, desired, metav1.CreateOptions{}); err != nil {
				r.Log.Error(err, "Failed to create SCC", "SCC", desired.Name)
				return err
			}
			r.Log.Info("Created SCC", "SCC", desired.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get SCC", "SCC", desired.Name)
		return err
	}

	needsUpdate := false

	if !equality.Semantic.DeepEqual(desired.Labels, actual.Labels) {
		actual.Labels = desired.Labels
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.AllowedCapabilities, actual.AllowedCapabilities) {
		actual.AllowedCapabilities = desired.AllowedCapabilities
		needsUpdate = true
	}
	if actual.RunAsUser.Type != desired.RunAsUser.Type ||
		(desired.RunAsUser.UID != nil && (actual.RunAsUser.UID == nil || *actual.RunAsUser.UID != *desired.RunAsUser.UID)) {
		actual.RunAsUser = desired.RunAsUser
		needsUpdate = true
	}
	if desired.Priority != nil && (actual.Priority == nil || *actual.Priority != *desired.Priority) {
		actual.Priority = desired.Priority
		needsUpdate = true
	}
	if actual.AllowPrivilegedContainer != desired.AllowPrivilegedContainer {
		actual.AllowPrivilegedContainer = desired.AllowPrivilegedContainer
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.Volumes, actual.Volumes) {
		actual.Volumes = desired.Volumes
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.AllowedUnsafeSysctls, actual.AllowedUnsafeSysctls) {
		actual.AllowedUnsafeSysctls = desired.AllowedUnsafeSysctls
		needsUpdate = true
	}
	if !equality.Semantic.DeepEqual(desired.SeccompProfiles, actual.SeccompProfiles) {
		actual.SeccompProfiles = desired.SeccompProfiles
		needsUpdate = true
	}
	// Preserve existing users/groups
	actual.Users = mergeStringSlices(actual.Users, desired.Users)
	actual.Groups = mergeStringSlices(actual.Groups, desired.Groups)

	if needsUpdate {
		// Note: Cannot set owner reference on cluster-scoped SCC from namespaced StorageCluster
		// SCC cleanup is handled by deleteBlackboxExporter()
		if _, err := r.SecurityClient.SecurityContextConstraints().Update(ctx, actual, metav1.UpdateOptions{}); err != nil {
			r.Log.Error(err, "Failed to update SCC", "SCC", actual.Name)
			return err
		}
		r.Log.Info("Updated SCC", "SCC", actual.Name)
	}

	return nil
}

func mergeStringSlices(existing, desired []string) []string {
	result := make([]string, len(existing))
	copy(result, existing)
	for _, item := range desired {
		if !slices.Contains(result, item) {
			result = append(result, item)
		}
	}
	return result
}

// createBlackboxConfigMap creates or updates the ConfigMap
func (r *StorageClusterReconciler) createBlackboxConfigMap(ctx context.Context, instance *ocsv1.StorageCluster) error {

	desired := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxConfigMapName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Data: map[string]string{
			"config.yml": `
modules:
  icmp_internal:
    prober: icmp
    timeout: 5s
    icmp:
      preferred_ip_protocol: ip4
`,
		},
	}

	actual := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Labels = desired.Labels
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Data = desired.Data
		return nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update ConfigMap for Blackbox Exporter")
		return err
	}

	r.Log.V(1).Info("ConfigMap for Blackbox Exporter is ready")
	return nil
}

// getCephClusterNetwork returns the Ceph cluster network configuration from OSD pod's networks annotation
// Returns empty slice if no networks annotation (use default SDN)
func (r *StorageClusterReconciler) getCephClusterNetwork(ctx context.Context, namespace string) ([]NetworkAttachment, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(namespace),
		client.MatchingLabels{"ceph_daemon_type": "osd"}); err != nil {
		return nil, err
	}

	if len(pods.Items) == 0 {
		return nil, fmt.Errorf("no OSD pods found in namespace %s", namespace)
	}

	// Read the networks annotation (Ceph cluster network configuration)
	networksJSON := pods.Items[0].Annotations["k8s.v1.cni.cncf.io/networks"]
	if networksJSON == "" {
		r.Log.V(1).Info("No networks annotation found on OSD pod, using default SDN",
			"pod", pods.Items[0].Name)
		return []NetworkAttachment{}, nil // Empty = use default SDN
	}

	var networks []NetworkAttachment
	if err := json.Unmarshal([]byte(networksJSON), &networks); err != nil {
		return nil, fmt.Errorf("failed to parse networks annotation: %w", err)
	}

	r.Log.Info("Found Ceph cluster network configuration", "networks", networks)
	return networks, nil
}

// buildMultusAnnotation creates the k8s.v1.cni.cncf.io/networks annotation value for Blackbox
// Format: "net1" for single network, or JSON array for multiple
func buildMultusAnnotation(networks []NetworkAttachment, namespace string) string {
	if len(networks) == 0 {
		return ""
	}

	if len(networks) == 1 {
		// Single network: use simple format
		net := networks[0]
		if net.Namespace != "" && net.Namespace != namespace {
			return fmt.Sprintf("%s/%s", net.Namespace, net.Name)
		}
		return net.Name
	}

	// Multiple networks: use JSON array format
	// Preserve namespace info for each network
	jsonBytes, err := json.Marshal(networks)
	if err != nil {
		return ""
	}
	return string(jsonBytes)
}

// getCephDaemonPodIPsFromNetworks extracts IPs only from specified networks
// If networks slice is empty, returns PodIP (default SDN)
func (r *StorageClusterReconciler) getCephDaemonPodIPsFromNetworks(
	ctx context.Context,
	namespace string,
	daemonType string,
	networks []NetworkAttachment,
) ([]string, error) {
	pods := &corev1.PodList{}
	if err := r.List(ctx, pods, client.InNamespace(namespace),
		client.MatchingLabels{"ceph_daemon_type": daemonType}); err != nil {
		return nil, err
	}

	var allIPs []string

	// If no networks specified, collect PodIPs (default SDN)
	if len(networks) == 0 {
		for _, pod := range pods.Items {
			if pod.Status.Phase != corev1.PodRunning {
				continue
			}
			if pod.Status.PodIP != "" {
				allIPs = append(allIPs, pod.Status.PodIP)
			}
		}
		r.Log.Info("Collected daemon pod IPs from default network",
			"type", daemonType,
			"count", len(allIPs))
		return allIPs, nil
	}

	for _, pod := range pods.Items {
		if pod.Status.Phase != corev1.PodRunning {
			continue
		}

		// Parse network-status annotation
		netStatusJSON, exists := pod.Annotations["k8s.v1.cni.cncf.io/network-status"]
		if !exists || netStatusJSON == "" {
			r.Log.V(1).Info("No network-status annotation, falling back to PodIP",
				"pod", pod.Name)
			if pod.Status.PodIP != "" {
				allIPs = append(allIPs, pod.Status.PodIP)
			}
			continue
		}

		var netStatusList []MultusNetStatus
		if err := json.Unmarshal([]byte(netStatusJSON), &netStatusList); err != nil {
			r.Log.V(1).Info("Failed to parse network-status, falling back to PodIP",
				"pod", pod.Name, "error", err)
			if pod.Status.PodIP != "" {
				allIPs = append(allIPs, pod.Status.PodIP)
			}
			continue
		}

		// Build a set of network names to match (handles namespace qualification)
		targetNetworks := make(map[string]bool)
		for _, net := range networks {
			// Add both qualified and unqualified forms for matching
			targetNetworks[net.Name] = true
			if net.Namespace != "" {
				targetNetworks[fmt.Sprintf("%s/%s", net.Namespace, net.Name)] = true
			}
		}

		// Extract IPs from matching networks only
		for _, netStatus := range netStatusList {
			// Check if this network is in our target list
			if !targetNetworks[netStatus.Name] {
				// Try matching unqualified name
				parts := strings.SplitN(netStatus.Name, "/", 2)
				if len(parts) == 2 && !targetNetworks[parts[1]] {
					continue // Not a target network
				} else if len(parts) == 1 && !targetNetworks[netStatus.Name] {
					continue // Not a target network
				}
			}

			// This is a target network - collect its IPs
			if len(netStatus.IPs) > 0 {
				allIPs = append(allIPs, netStatus.IPs...)
				r.Log.V(1).Info("Extracted IPs from target network",
					"pod", pod.Name,
					"network", netStatus.Name,
					"ips", netStatus.IPs)
			}
		}
	}

	r.Log.Info("Collected daemon pod IPs from specified networks",
		"type", daemonType,
		"count", len(allIPs),
		"networks", networks)

	return allIPs, nil
}

// createBlackboxDeployment deploys the Blackbox Exporter
func (r *StorageClusterReconciler) createBlackboxDeployment(ctx context.Context, instance *ocsv1.StorageCluster, podAnnotations map[string]string) error {
	placement := GetPlacement(instance, defaults.BlackboxExporterKey)

	desired := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: ptr.To(int32(1)),
			Selector: &metav1.LabelSelector{MatchLabels: blackboxExporterLabels},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      blackboxExporterLabels,
					Annotations: podAnnotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: blackboxServiceAccount,
					Tolerations:        placement.Tolerations,
					Affinity: &corev1.Affinity{
						NodeAffinity: placement.NodeAffinity,
					},
					SecurityContext: &corev1.PodSecurityContext{
						Sysctls: []corev1.Sysctl{
							{
								Name:  "net.ipv4.ping_group_range",
								Value: "0 2147483647",
							},
						},
					},
					Containers: []corev1.Container{
						{
							Name:  blackboxExporterName,
							Image: r.images.BlackboxExporter,
							Args: []string{
								"--config.file=/etc/blackbox_exporter/config.yml",
								"--web.listen-address=:9115",
							},
							Ports: []corev1.ContainerPort{
								{
									Name:          portBlackboxHTTP,
									ContainerPort: blackboxPortNumber,
									Protocol:      corev1.ProtocolTCP,
								},
							},
							VolumeMounts: []corev1.VolumeMount{
								{
									Name:      "config",
									MountPath: "/etc/blackbox_exporter",
									ReadOnly:  true,
								},
							},
							SecurityContext: &corev1.SecurityContext{
								RunAsNonRoot: ptr.To(true),
								Capabilities: &corev1.Capabilities{
									Add: []corev1.Capability{"NET_RAW"},
								},
								AllowPrivilegeEscalation: ptr.To(false),
								SeccompProfile: &corev1.SeccompProfile{
									Type: corev1.SeccompProfileTypeRuntimeDefault,
								},
							},
							Resources: getDaemonResources(blackboxExporterName, instance),
						},
					},
					Volumes: []corev1.Volume{
						{
							Name: "config",
							VolumeSource: corev1.VolumeSource{
								ConfigMap: &corev1.ConfigMapVolumeSource{
									LocalObjectReference: corev1.LocalObjectReference{
										Name: blackboxConfigMapName,
									},
								},
							},
						},
					},
					PriorityClassName: systemClusterCritical,
				},
			},
		},
	}
	actual := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err := controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		if actual.CreationTimestamp.IsZero() {
			actual.Spec.Selector = &metav1.LabelSelector{MatchLabels: blackboxExporterLabels}
			if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
				return err
			}
		}
		actual.Spec.Template = desired.Spec.Template
		return nil
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update Deployment for Blackbox Exporter")
		return err
	}

	return nil
}

// createBlackboxService exposes the exporter
func (r *StorageClusterReconciler) createBlackboxService(ctx context.Context, instance *ocsv1.StorageCluster) error {
	expected := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: corev1.ServiceSpec{
			Selector: blackboxExporterLabels,
			Ports: []corev1.ServicePort{
				{
					Name:       portBlackboxHTTP,
					Port:       blackboxPortNumber,
					TargetPort: intstr.FromInt(blackboxPortNumber),
					Protocol:   corev1.ProtocolTCP,
				},
			},
			Type: corev1.ServiceTypeClusterIP,
		},
	}
	actual := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      expected.Name,
			Namespace: expected.Namespace,
		},
	}

	err := r.Get(ctx, types.NamespacedName{Name: actual.Name, Namespace: actual.Namespace}, actual)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// Create new Service
			if err := controllerutil.SetControllerReference(instance, expected, r.Scheme); err != nil {
				r.Log.Error(err, "Failed to set owner reference on Service", "Service", expected.Name)
				return err
			}
			if err := r.Create(ctx, expected); err != nil {
				r.Log.Error(err, "Failed to create Service", "Service", expected.Name)
				return err
			}
			r.Log.Info("Created Service", "Service", expected.Name)
			return nil
		}
		r.Log.Error(err, "Failed to get Service", "Service", expected.Name)
		return err
	}

	// Preserve ClusterIP across updates
	if actual.Spec.ClusterIP != "" {
		expected.Spec.ClusterIP = actual.Spec.ClusterIP
	}

	// Only update if the labels or spec differ
	if !equality.Semantic.DeepEqual(expected.Labels, actual.Labels) ||
		!equality.Semantic.DeepEqual(expected.Spec, actual.Spec) {

		actual.Labels = expected.Labels
		actual.Spec = expected.Spec

		if err := controllerutil.SetControllerReference(instance, actual, r.Scheme); err != nil {
			r.Log.Error(err, "Failed to set owner reference on Service", "Service", actual.Name)
			return err
		}
		if err := r.Update(ctx, actual); err != nil {
			r.Log.Error(err, "Failed to update Service", "Service", actual.Name)
			return err
		}
		r.Log.Info("Updated Service", "Service", actual.Name)
	}

	return nil
}

// buildIPRegex creates a Prometheus-compatible regex matching any IP in the list
// Input: ["10.0.0.1", "10.0.0.2"] → Output: "^(10\\.0\\.0\\.1|10\\.0\\.0\\.2)$"
func buildIPRegex(ips []string) string {
	if len(ips) == 0 {
		return "$^" // Match nothing (invalid regex that never matches)
	}
	var escaped []string
	for _, ip := range ips {
		// Escape dots for regex + wrap in quotes for Prometheus regex syntax
		escaped = append(escaped, strings.ReplaceAll(ip, ".", "\\."))
	}
	// Prometheus regex uses alternation with quoted strings
	return "^(" + strings.Join(escaped, "|") + ")$"
}

// createBlackboxProbe creates or updates the Probe with relabeling for daemon_type labels
func (r *StorageClusterReconciler) createBlackboxProbe(ctx context.Context, instance *ocsv1.StorageCluster, cephClusterNetwork []NetworkAttachment) error {

	osdIPs, err := r.getCephDaemonPodIPsFromNetworks(ctx, instance.Namespace, "osd", cephClusterNetwork)
	if err != nil {
		r.Log.Error(err, "Failed to get OSD pod IPs")
		return err
	}

	monIPs, err := r.getCephDaemonPodIPsFromNetworks(ctx, instance.Namespace, "mon", cephClusterNetwork)
	if err != nil {
		r.Log.Error(err, "Failed to get MON pod IPs")
		return err
	}

	allTargets := append(osdIPs, monIPs...)
	if len(allTargets) == 0 {
		r.Log.Info("No OSD or MON pod IPs found, skipping Probe creation")
		return nil
	}

	var metricRelabelConfigs []monitoringv1.RelabelConfig

	if len(osdIPs) > 0 {
		metricRelabelConfigs = append(metricRelabelConfigs, monitoringv1.RelabelConfig{
			SourceLabels: []monitoringv1.LabelName{"instance"},
			Regex:        buildIPRegex(osdIPs),
			TargetLabel:  "daemon_type",
			Replacement:  ptr.To("osd"),
			Action:       "replace",
		})
	}

	if len(monIPs) > 0 {
		metricRelabelConfigs = append(metricRelabelConfigs, monitoringv1.RelabelConfig{
			SourceLabels: []monitoringv1.LabelName{"instance"},
			Regex:        buildIPRegex(monIPs),
			TargetLabel:  "daemon_type",
			Replacement:  ptr.To("mon"),
			Action:       "replace",
		})
	}

	desired := &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      blackboxExporterName,
			Namespace: instance.Namespace,
			Labels:    blackboxExporterLabels,
		},
		Spec: monitoringv1.ProbeSpec{
			ProberSpec: monitoringv1.ProberSpec{
				URL:  fmt.Sprintf("%s.%s.svc:%d", blackboxExporterName, instance.Namespace, blackboxPortNumber),
				Path: "/probe",
			},
			Module: "icmp_internal",
			Targets: monitoringv1.ProbeTargets{
				StaticConfig: &monitoringv1.ProbeTargetStaticConfig{
					Targets: allTargets,
				},
			},
			Interval:             blackboxScrapeInterval,
			ScrapeTimeout:        "10s",
			MetricRelabelConfigs: metricRelabelConfigs,
		},
	}

	actual := &monitoringv1.Probe{
		ObjectMeta: metav1.ObjectMeta{
			Name:      desired.Name,
			Namespace: desired.Namespace,
		},
	}

	_, err = controllerutil.CreateOrUpdate(ctx, r.Client, actual, func() error {
		// Preserve authorization set by CRD defaulting or an external controller;
		// this reconciler does not own auth on the Probe.
		existingAuthorization := actual.Spec.Authorization

		actual.Spec = desired.Spec

		if desired.Spec.Authorization == nil {
			actual.Spec.Authorization = existingAuthorization
		}

		return controllerutil.SetControllerReference(instance, actual, r.Scheme)
	})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		r.Log.Error(err, "Failed to create/update Probe for Blackbox Exporter")
		return err
	}

	r.Log.Info("Blackbox Probe created/updated successfully",
		"targets", len(allTargets),
		"osd", len(osdIPs),
		"mon", len(monIPs))

	return nil
}
